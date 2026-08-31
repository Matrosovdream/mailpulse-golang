# 04 — Decisions already made

Living doc. The point is that these do not get relitigated every few weeks —
if one is revisited, edit the entry and say what changed.

---

## Architecture

**Filters are rows, not a JSON tree.** One row per condition in
`watcher_filters` plus `match_mode` (all/any). A nested AND/OR tree in `jsonb`
supports more, at the cost of a custom tree editor in the UI and unreadable
debugging. If nested groups become real, add a nullable `group_id` to the rows
— it upgrades without a rewrite.

**Sessions, not a token column.** The reference scaffold kept one token on the
user row, so logging in on a phone killed the desktop session. `user_sessions`
stores a SHA-256 hash; the raw token never touches the database.

**Roles are a pivot.** `roles` + `user_roles`, seeded with fixed UUIDs
(`…0001` user, `…0002` superadmin). Roles ride along in the cached `Auth`, so
a role check costs no query — which is why changing roles must evict sessions.

**Repeats are rows, not a sleeping goroutine.** Every occurrence is an
`event_runs` row with `scheduled_at`. Restarts, multiple workers, and
"cancel the remaining three" all fall out of that for free.

**Dedupe at the match, not the send.** Unique `(watcher_id, message_id)`.
IMAP re-delivers; the alternative is duplicate 3am notifications.

**Notifier links are a join table.** `watcher_event_notifiers` with real FKs,
so deleting a notifier that is in use returns a 409 naming the watchers rather
than leaving dangling ids in a JSON blob.

**Optional capabilities via type assertion** (planned, [01](01-mail-integrations.md)) —
`FolderLister`, `PushProvider`, `TokenRefresher` are separate interfaces so an
API provider is not forced to stub IMAP concepts.

**The auth cache is keyed by token hash, not the raw token.** Revocation only
ever holds `user_sessions.token_hash`, so a cache keyed by the raw token had no
way to find the entry to evict. Keying both by the same hash is what makes
revocation immediate; it also keeps usable bearer tokens out of redis key names.

**Impersonation is resolved centrally, not at each call site.** The middleware
puts `Auth` on the context and `AuditUseCase.Record` rewrites the actor. Passing
the pair into all 26 `Record` calls was the alternative, and is exactly the
arrangement that let the original bug exist — one forgotten call site silently
misattributes. A new audit line now cannot get it wrong.

**Rate limits are per IP and fail open.** A fixed window in redis, generous
enough for an office behind one NAT address. If redis is unreachable the request
is allowed: the counter is a brake on guessing, not an authorisation decision,
and failing closed converts a cache outage into a full outage. The stronger
control — a per-account counter on failures only — is deliberately deferred, and
is the reason the per-IP number is not tighter.

**`X-Forwarded-For` is only honoured from `WEB_TRUSTED_PROXIES`.** Fiber's
trusted-proxy check is on, so `ctx.IP()` is the peer unless the peer is a
configured proxy. Taking the header at face value, as `clientIP` used to, let
any caller choose the IP written to the audit trail and gave the rate limiter an
identity the attacker picks.

**OAuth token refresh lives in `MailResolver.Resolve`, not in a method callers
opt into.** There are four ways into a mailbox — `Verify`, `Folders`, the
credential re-check job and the sync pipeline — and a refresh each of them has
to remember is one forgotten call site away from a mailbox that silently stops
an hour after it is connected. Same reasoning as impersonation being resolved
centrally: there is no second door to forget.

**A refresh is committed on its own handle, not the caller's transaction.**
Providers retire the old refresh token the moment they issue a new one, so a
renewal that gets rolled back with the caller's work leaves the account holding
a credential the provider has already thrown away — unrecoverable without asking
the user to consent again. Parking a revoked account is committed the same way,
and for a sharper reason: `Verify` rolls back when the resolver fails, so
routing that write through its transaction meant the account stayed `pending`
and was retried forever. There is a feature test that fails if this is undone.

**The OAuth state is consumed with `GETDEL`.** A callback URL carries a working
authorization code and lands in browser history, server logs and referrer
headers, so it gets replayed — sometimes simultaneously. Read-then-delete has a
window where two replays both see the state and both spend the code. One atomic
command means exactly one winner and the loser is told the link expired.

**The OAuth registry is keyed by provider slug, the mail registry by kind.**
They look like they should match and should not: which consent screen a mailbox
uses is a property of the service, while which client opens it is a property of
the transport. `gmail`, `outlook` and `yandex` are three consent screens sharing
one IMAP client.

**The callback answers with a redirect, always, and never forwards the
provider's words.** It is reached by a provider redirecting a browser, so there
is nothing on the other end that would read a JSON error body. Failures come
back as a short code of our own — `denied`, `state`, `exchange` and so on —
because the provider's error text is attacker-influenced and would end up
reflected into the page.

## Data

**`bigint` epoch millis, not `timestamptz`.** Carried over from the reference
architecture for consistency with the existing entities. Noted because it is
the kind of thing that looks like an oversight; it was a choice.

**Check constraints, not Postgres enums.** Adding a value is a migration you
can run without locking. Exception: `mail_accounts.provider` should become a
FK to a seeded table, because it changes every time an integration is added.

**Credentials are encrypted, with no default key.** AES-GCM under
`SECURITY_ENCRYPTION_KEY`; the app refuses to start without it. A shipped
default would read as protection while providing none. **Losing the key makes
stored mailbox credentials unrecoverable.**

## Mail client

**Read-only.** Readonly `SELECT` and `BODY.PEEK`, so watching a mailbox never
marks anything as read. This is why `forward_email` and `tag` are out of scope
— they need write access, and that is a deliberate later decision.

**go-imap v1.2.1, not v2.** v2 is still `beta.8`. Stability wins for code that
holds other people's mailbox credentials.

**UIDVALIDITY change resets the cursor** rather than trusting a stale UID —
the alternative silently skips mail that was never seen.

**First sync takes the newest 200 by sequence**, not the whole mailbox.
`watch_from` defaults to creation time so a new watcher does not fire on a
year of history.

**STARTTLS is opportunistic.** On a cleartext port we upgrade if the server
offers it (Yandex and Gmail both do on 143), and hard-fail if the upgrade
breaks. Where no TLS exists at all we still connect but log
`sending credentials in cleartext` — required for GreenMail in dev.

## OAuth

**A reconnection that comes back as a different mailbox is refused, not
absorbed.** `_reauthorize` pins the account id into the OAuth state, so
`connect` finds the row by id and treats the address the provider returns as a
check. When they differ the callback answers `oauth_error=mismatch` and writes
nothing.

The two alternatives were both worse. Falling back to matching on the returned
address makes the button do something other than what it says — the user asked
to reconnect *this* mailbox. Letting the repoint through is the real hazard:
the row owns watchers, matches and a sync cursor built from one mailbox, and
carrying all of that onto another silently is a data problem the user has no
way to see, let alone undo. Through `_authorize` the same mis-click is
harmless — it creates a second, visible, deletable mailbox — which is exactly
why the pinned path needs the guard and the unpinned one does not.

**A mailbox on an app password may be re-authorised into OAuth.** `_reauthorize`
does not require `auth_mode = xoauth2`, only that the provider supports it.
Consent is precisely how such an account stops needing an app password, and
`connect` already set `AuthMode = xoauth2` unconditionally, so re-consenting
through `_authorize` did this before there was a button for it.

**`_reauthorize` does not revoke the old grant.** It returns a consent URL and
nothing else. Providers retire the previous refresh token when they issue the
next one, and the merge rule below (never rebuild, never overwrite a stored
refresh token with an empty one) already covers the window in between. Calling
a revocation endpoint first would leave a mailbox with no working credential if
the user then abandoned the consent screen.

## Redis and remote infrastructure

**Postgres and Redis are addressed by env, so either can live on another
machine.** `DATABASE_HOST` and `REDIS_HOST` take a hostname or an IP; nothing
in the code assumes they are in the compose file. What a remote one additionally
needs is configured rather than assumed: `REDIS_TLS` (every managed Redis
refuses plaintext), `REDIS_USERNAME` for Redis 6 ACLs, explicit dial/read/write
timeouts instead of the library's LAN-shaped defaults, `REDIS_CONNECT_ATTEMPTS`
so a restart on the other side does not take this process down at boot, and
`DATABASE_SSLROOTCERT` so `verify-full` has a CA to verify against.

`docker-compose.prod.yml` publishes no ports for either service and starts
Redis with `--requirepass ${REDIS_PASSWORD:?...}`, so the co-located case fails
fast rather than running open. The dev stack publishes 6379 with no password,
which is correct bound to a laptop and would be an open Redis on a public IP.

**The mail_providers row is cached; the filter list is not.** Both were read far
too often, but only one of them should be a cache. The provider row is seven
static rows read once per account per poll — a genuine cache, keyed by slug,
TTL `REDIS_TTL_MAIL_PROVIDER`. The filters were an N+1 inside the message loop,
and the fix was to load them once per sync, not to memoise the mistake. The
general rule: cache what is genuinely shared and rarely changes; fix what is
merely queried badly.

**The provider cache stores misses as well as hits.** An unknown slug is a 400,
and without a negative entry a client retrying one would reach the database on
every attempt — the case where a hits-only cache adds the load it was meant to
remove.

**An unreachable provider cache falls through to the database**, and never
reports a provider as unknown. `Lookup` returns a third value separating "not
cached" from "cached as missing" precisely so a Redis outage cannot park every
mailbox in the fleet at once.

**It is TTL-only: nothing invalidates it.** `Forget` exists but has no caller,
so a `make seed` that changes a provider row takes up to `REDIS_TTL_MAIL_PROVIDER`
to reach the poller. The HTTP write paths in `MailAccountUseCase` deliberately
still read through, so an edit is visible immediately there. Wire `Forget` into
the seeder if provider rows ever change outside a deploy.

## Bugs found by verification — keep these fixed

**IMAP `N:*` returns the newest message even when N is past the highest UID.**
Every poll re-fetched the last email forever; dedupe hid it but the work was
real. Fixed by filtering on UID after the fetch.

**Rebuilding the credentials blob wiped the username.** `PATCH` with only a
password destroyed `imap_username`, and since it is never returned to the
client it was unrecoverable. Credentials are now decrypt-merge-encrypt.

**The same shape again, with refresh tokens.** Google returns a refresh token
only on the first consent and omits it from every later one, and from every
refresh. Assigning it unconditionally strips the account's only way to renew,
and the failure surfaces an hour later looking exactly like a revoked grant. An
empty refresh token now means "keep the one you have", in both the callback and
the resolver. Two feature tests hold this in place.

**`revokeAllSessions` threw away the hashes it needed.**
`RevokeAllForUser` returns the revoked token hashes specifically so the caller
can evict them, and the caller assigned them to `_`. Every path that revokes on
a credential change — password reset, password update, suspension, role change —
left the old sessions authenticating from cache for up to `REDIS_TTL_AUTH`. The
role-change case is the sharp one: `04` says roles ride along in the cached
`Auth`, and this was the eviction that was supposed to make that safe.

**`attempt` never incremented.** The dispatcher bumped it in SQL while the
in-memory struct kept the old value and wrote it back. A permanently failing
run would have retried forever at a constant backoff.

## Environment

**Config is env-only.** `config.json` was removed; defaults live in
`internal/config/viper.go`, `.env` is loaded by godotenv so one file works both
in a container and from the shell. Compose overrides only the three hostnames.

**GreenMail is a dev dependency, not a test double.** The IMAP client is
exercised against a real server. Note `-Dgreenmail.auth.disabled` must *not*
be set — it makes GreenMail auto-create users, which collides with declared
ones.

**`MAIL_STUB_ENABLED`** still selects a fake provider for demos, defaults to
`false`, and logs a loud warning when on.
