# 03 — Platform gaps

**Status:** tests done (three layers, all green), the three security bugs fixed,
and the API spec plus CORS shipped (2026-08-20). Worker is untouched; ops is
down to healthchecks, the migrate targets and README; CI landed 2026-08-31.
Everything here is known-missing rather than discovered-later, and each item
has been observed, not assumed.

---

## Security

- [x] **Impersonation is attributed.** `user_sessions.impersonated_by`
      (`20260819000000_add_session_impersonation`) marks the session at issue
      time; `Verify` carries it into `Auth`; the middleware puts `Auth` on the
      context and `AuditUseCase.attribute` rewrites the actor centrally, so an
      action taken while impersonating records the admin with the target in
      `impersonated_user_id`. Done in one place rather than at the 26 `Record`
      call sites, because spreading it out is what produced the bug. Chaining
      impersonation is refused: a session records only one impersonator.
      `_impersonate` is now safe to use in production.
- [x] **Revocation evicts the cache.** The cache is keyed by token hash instead
      of the raw token, which is what makes eviction possible — a session row
      only ever holds the hash. Fixed `RevokeSession` *and* `revokeAllSessions`,
      which was discarding the hashes the repository already returned for this
      purpose, so a password change or a role change left every session live for
      up to `REDIS_TTL_AUTH`. Side benefit: a redis dump no longer yields usable
      bearer tokens as key names.
- [x] Rate-limit `/api/users/_login` and `_forgot-password` — fixed-window Redis
      counter, per IP, fails open if Redis is unreachable. Keyed on `ctx.IP()`
      behind `EnableTrustedProxyCheck`, so a forged `X-Forwarded-For` cannot buy
      more attempts; `WEB_TRUSTED_PROXIES` names who may set it.
- [ ] Per-account login counter on *failures only*. The per-IP limit has to be
      generous enough for an office behind one NAT address, so it slows spraying
      rather than stopping targeted guessing. Needs the counter inside the
      handler, where success is known.
- [ ] `credentials` and `secrets` are AES-GCM encrypted, but there is no key
      rotation story. Decide before the first production key.

## Tests — three layers, split by build tag

Layout and conventions live in [test/README.md](../../test/README.md): unit
beside the code (`go test ./internal/...`, no infrastructure), integration and
feature under `test/` behind `integration` / `feature` tags so `go test ./...`
never needs a database.

- [x] `usecase/matcher_test.go` — pure functions, no database, highest value
      per line: every field × operator, `all` vs `any`, case sensitivity,
      invalid regex, header lookup
- [x] `gateway/secret` cipher — round trip, tampering, wrong key
- [x] `test/integration/` — repositories and seeders against real Postgres and
      Redis: dedupe index, `SKIP LOCKED` claims, roles pivot, seeder idempotency
- [x] `test/feature/` — 26 tests / 183 assertions over the whole app: auth,
      tenant isolation, catalogs, mail accounts, watchers, and an end-to-end
      pipeline that delivers real mail over SMTP and follows the match through
      to delivery. Supersedes the 78-assertion smoke script.
- [x] `test/feature/security_test.go` — session revocation, impersonation
      attribution and both rate limits. Each was checked against the old
      behaviour and confirmed to fail, which caught one that passed for the
      wrong reason: it never cached the token it went on to revoke, so the
      request fell through to the database and would have been rejected either
      way.
- [ ] `gateway/mail/imap` — cursor maths incl. the `N:*` overlap guard
      (the one `04-decisions.md` bug with no regression test)
- [ ] Dispatcher: retry/backoff, `repeat_max`, `stop_on_ack`, cancel-after —
      `repeat_max` is only ever set in a create payload, never exercised

## Worker

- [ ] Cron-expression recurrence is stored and validated but never expanded —
      the dispatcher only handles `repeat_interval_seconds` and logs a warning.
      Needs a cron library and the user's timezone.
- [ ] Kafka carries nothing. `email.matched` between sync and dispatch was the
      design; today it is a direct DB handoff. Either wire it or drop Kafka
      from the stack — it is currently decoration.
- [ ] No dead-letter path for runs that exhaust `max_attempts`.
- [ ] Per-channel rate limiting (Telegram will throttle).

## API / SPA

- [x] `GET /api/mail-provider-types` — see [01](01-mail-integrations.md#6-api-surface)
- [ ] `_verify` / `_sync` are synchronous and can block for
      `MAIL_IMAP_TIMEOUT` (30s). Queue + poll.
- [ ] Telegram `stop_on_ack` has no button: `_ack` exists, but the webhook
      ignores `callback_query`, so there is nothing for the user to tap.
- [ ] `forward_email` and `tag` event types (need IMAP write access —
      deliberately out of scope while the client is read-only).
- [ ] Password reset emails are logged, not sent.

## Ops

- [ ] `make migrate-up` / `migrate-down` / `migrate-version` are all broken:
      `docker compose run --rm migrate up` replaces the service's whole
      `command`, dropping `-path` and `-database`, so every target dies with
      "URL cannot be empty". Migrations only apply because the stack runs the
      service as a `depends_on` at startup. Fix by putting the flags in an
      `entrypoint` rather than `command`, so `run` only appends the verb.
- [ ] No container healthcheck on `web` / `worker` — `/api/health` now exists,
      so compose can use it.
- [x] ~~`api/api-spec.json` is empty~~ — landed as `api/openapi.yaml` (YAML, not
      JSON: it is hand-written, and the comments and prose are the point).
      OpenAPI 3.1, all 75 routes, embedded via `go:embed` and served at
      `/api/docs` (Swagger UI) and `/api/openapi.yaml`, both behind
      `WEB_DOCS_ENABLED`. Observed working: 75/75 route-to-operation diff,
      `redocly lint` clean, 14/14 live responses validated against their
      schemas with ajv, and `orval` + `openapi-typescript` both generate a
      usable client.
- [ ] `README.md` is still one line.
- [x] ~~**No CORS anywhere**~~ — done 2026-08-20. `middleware.NewCORS` mounted
      by `RouteConfig.SetupCORS` **before every other group**, which is the
      whole point: a browser sends its preflight without `Authorization`, so
      while the `/api` group's auth middleware got there first it answered
      `401` and the real request was never sent — the API worked from curl and
      not from a browser. Allowlist from `WEB_CORS_ORIGINS` (CSV); empty means
      the middleware is **not mounted at all**, because fiber reads an empty
      `AllowOrigins` as `"*"` and a wildcard is not acceptable with bearer
      tokens. `AllowCredentials` stays false (bearer token, not a cookie);
      `Retry-After` is exposed so the SPA can read the rate-limit wait.
      Observed working: preflight `401` → `204` with the origin echoed, an
      authenticated `GET` carrying `Access-Control-Allow-Origin`, and a
      non-allowlisted origin getting no such header. Covered by 6 unit tests
      next to the middleware and 4 feature tests through the real route table
      (the latter skip when `WEB_CORS_ORIGINS` is unset, rather than passing
      vacuously). No new dependency — `cors` ships inside fiber v2.
- [ ] `.env.prod` must be created before the prod stack starts.
- [x] ~~No CI pipeline.~~ — `.github/workflows/ci.yml`, three jobs, 2026-08-31.
      `check` runs gofmt, `go vet` under all three tag sets and a `go mod tidy`
      diff — `./...` on its own never sees the tagged test packages, so a
      broken `test/` used to compile green. `test` runs all three layers
      against Postgres, Redis and GreenMail as service containers, migrations
      applied first by the same pinned `migrate/migrate:v4.18.1` compose uses:
      the `roles` and `mail_providers` rows the feature suite reads come from
      the migrations, so `cmd/seeder` never runs. No Kafka service — the
      harness wires `Producer: nil`. `image` builds the `prod` target on every
      event, so a pull request proves the Dockerfile still builds, and pushes
      to `ghcr.io/matrosovdream/mailpulse-golang` from `main` and `v*` tags
      only. One image rather than two: the `prod` stage carries both binaries
      and compose only varies the entrypoint between `web` and `worker`.
      **No deploy step** — there is no host yet; when there is, it is one more
      job.
