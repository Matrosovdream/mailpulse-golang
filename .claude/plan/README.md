# MailPulse — plan

Tracking board. Tick boxes as work lands; keep the status line at the top of
each file current.

| # | Area | File | Status |
|---|------|------|--------|
| 01 | Mail integrations (providers, auth modes, schema) | [01-mail-integrations.md](01-mail-integrations.md) | foundation done |
| 02 | OAuth & token management | [02-oauth.md](02-oauth.md) | built; awaiting a real app registration |
| 03 | Platform gaps (tests, security, worker) | [03-platform-gaps.md](03-platform-gaps.md) | tests + security done; worker/ops open |
| 04 | Decisions already made | [04-decisions.md](04-decisions.md) | living doc |
| 05 | Load and performance testing | [05-load-testing.md](05-load-testing.md) | running; first measurements in |

## Where the system is today

Working end to end, verified against a real IMAP server:

- [x] Clean-architecture skeleton — entity / model / repository / usecase / delivery / gateway
- [x] 15 tables, 11 migrations, verified up + down against Postgres 16
- [x] 75 HTTP routes, 78/78 smoke assertions passing
- [x] Auth: sessions, roles via `user_roles` pivot, Redis-cached verification
- [x] Three plug-in registries: event handlers, notifier channels, mail providers
- [x] Pipeline: sync → match → schedule → dispatch → deliver, incl. repeat chains
- [x] Generic IMAP client (real, read-only, UID cursor, MIME + Cyrillic verified)
- [x] Credential re-check job (hourly, marks broken accounts and auto-recovers)
- [x] Docker dev + prod stacks, env-only config, AES-GCM credential encryption
- [x] Provider registry keyed by kind, `mail_providers` table, `GET /api/mail-provider-types`
- [x] Three-layer test suite — unit / integration / feature, split by build tag;
      all three layers green
- [x] Impersonation attributed to the acting admin, revocation evicts the auth
      cache, credential endpoints rate-limited (`20260819000000`)
- [x] `api/openapi.yaml` — hand-written OpenAPI 3.1 covering all 75 routes,
      embedded and served at `/api/docs` + `/api/openapi.yaml`
      (`WEB_DOCS_ENABLED`). Verified: route table diffs 75/75 against the spec,
      14/14 live responses validate against their schemas, and both
      `openapi-typescript` and `orval` generate a working client from it
- [x] CORS via an explicit `WEB_CORS_ORIGINS` allowlist, mounted ahead of the
      auth group so browser preflights are answered instead of 401'd
- [x] OAuth connect flow — consent, single-use state, code exchange, encrypted
      token storage, lazy refresh under a per-account lock, and revocation
      parking the mailbox. Google, Microsoft and Yandex clients
- [x] XOAUTH2 on the IMAP connect path, so an OAuth mailbox needs no app
      password. Hand-written SASL mechanism: go-sasl ships OAUTHBEARER only
- [x] `POST /api/mail-accounts/:id/_reauthorize` reconnects a parked mailbox.
      The state carries the account id, so the callback updates that row; a
      consent screen naming a different mailbox is refused with
      `oauth_error=mismatch` rather than repointing the row at it
- [x] `cmd/loadtest` — three suites (`endpoints`, `worker`, `parser`) over a
      shared runner/report/fixtures/guard layer, plus a `loadstub` mail
      provider with a lognormal latency distribution and benchmarks beside the
      code. `make load` lists them. Four findings so far, in
      [05](05-load-testing.md): the poller holds ~226 accounts but only ~19 if
      one mailbox in twenty hangs, `admin-users` is 136x slower than its
      neighbours, and the matcher recompiles its regex on every call
- [x] CI on GitHub Actions (`.github/workflows/ci.yml`) — gofmt, `go vet`
      under all three tag sets and a `go mod tidy` diff; all three test layers
      against Postgres, Redis and GreenMail service containers; and the `prod`
      image published to `ghcr.io/matrosovdream/mailpulse-golang` from `main`
      and `v*` tags. Nothing deploys: there is no host yet

Verified only against a fake provider, not a real one:

- [ ] OAuth end to end through a live consent screen. No application is
      registered with Google, Microsoft or Yandex, so nothing above has met a
      real provider. The feature suite drives the production code path against
      a stub (`OAUTH_STUB_URL`), and GreenMail advertises no SASL mechanisms so
      the XOAUTH2 handshake itself is unit-tested rather than exercised.
      **Yandex is the cheap way to close this** — registration takes minutes and
      needs no review ([02](02-oauth.md))

Not working yet, in rough priority order:

- [ ] Cron-expression recurrence
- [ ] Kafka carries nothing in the pipeline
- [ ] No metrics of any kind. Worker capacity **has** now been measured
      ([05](05-load-testing.md)); production still cannot be watched

## Execution order

Agreed 2026-08-19. Each file carries its own internal ordering; this is the
sequence across them.

0. **App registration** — no code, but calendar-bound, and now the only thing
   between OAuth and a ticked box. Google needs `https://mail.google.com/`, not
   `gmail.readonly` (see [02](02-oauth.md)), and that assessment has weeks of
   lead time. **Yandex takes minutes and needs no review**, so do it first: it
   is what proves the flow against something real.
1. ~~**Security**~~ — impersonation attribution, revoke cache eviction, rate
   limits. Done ([03](03-platform-gaps.md)); defects in shipped features, so
   they went before new capability.
2. ~~**OAuth foundation**~~ — `internal/gateway/oauth/`, `OAuthStateCache`,
   token storage, lazy refresh. Done ([02](02-oauth.md)).
3. ~~**XOAUTH2 over the existing IMAP provider**~~ — done, together with step 2,
   because step 2 alone stores tokens nothing can spend and so could never be
   ticked under the convention below. Note `go-sasl` did **not** ship the
   mechanism; it is hand-written.
4. ~~**`_reauthorize`**~~ — done ([02](02-oauth.md)). Small, because
   `connect` already merged onto an existing row; what it adds is the pin from
   `OAuthState.AccountID` and the refusal when consent comes back naming a
   different mailbox.
5. Gmail API provider → 6. Graph provider → 7. push subscriptions and the
   renewal loop.
8. **Worker** — dispatcher tests first, then cron expansion, dead-letter path,
   Telegram `callback_query`.
9. **Ops** — healthchecks on `web`/`worker`, the migrate targets, README.
10. **Inbound relay** — last, nothing needs it yet.

Load and performance work ([05](05-load-testing.md)) is sequenced separately
and is now underway. Its stated prerequisite — metrics before anything else —
turned out to be wrong for capacity work and was corrected in place: a harness
that drives the loop is its own instrument. Metrics are still needed to watch
production.

The measurements put four defects on the table. Two are fixed: the filter N+1
in the poll loop (3.2x on cycle time) and the `admin-users` N+1 (12.6x on the
endpoint). Two remain: the **missing per-item deadline in the poller** — the
largest of them, since one hung mailbox in twenty costs 92% of a worker's
capacity — and the **regex recompiled on every match evaluation**.

Two open decisions belong to whoever owns the product, not to the next commit:
the **Kafka** call in [03](03-platform-gaps.md) (wire `email.matched` or drop
it), and **encryption key rotation** in [04](04-decisions.md). Rotation got more
urgent with this step: the encrypted blob now holds long-lived refresh tokens,
so losing the key means every connected mailbox has to consent again.

## Conventions

- A box is ticked only when the thing has been **run and observed working**,
  not when the code merely compiles.
- Anything that needs a migration names the migration file.
- Anything deliberately *not* done goes in [04-decisions.md](04-decisions.md)
  with the reason, so it does not get relitigated every few weeks.
