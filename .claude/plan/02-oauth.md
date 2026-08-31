# 02 — OAuth & token management

**Status:** built, verified against a fake provider, **not yet verified against a
real one** — no application is registered with Google, Microsoft or Yandex, so
no box below is ticked that depends on a live consent screen. The flow works end
to end in the feature suite: consent, exchange, encrypted storage, lazy refresh,
revocation handling, reconnection, and XOAUTH2 selected on the IMAP connect
path.

OAuth is split out from [01](01-mail-integrations.md) because it is the
prerequisite for Gmail and Graph, it is mostly *not* mail-specific, and the
Google verification step has weeks of lead time that should start early.

---

## Corrections to this document, found while building it

**go-sasl ships no XOAUTH2 client.** The pinned `go-sasl` has `oauthbearer.go`
(OAUTHBEARER, RFC 7628) and nothing else. XOAUTH2 predates the RFC, uses a
different mechanism name and a different initial response, and is the one every
mail provider actually advertises on IMAP. It is hand-written in
`internal/gateway/mail/imap/xoauth2.go` — about twenty lines against
`sasl.Client`, which beats forking the dependency. No new module either way.

**`gmail.readonly` does not grant IMAP access.** It grants the Gmail API. IMAP
with XOAUTH2 needs `https://mail.google.com/`, which is *also* restricted — same
security assessment, different string. The narrow scope buys nothing here, it
just fails later at connect time. The app registration must ask for the broad
one.

**Microsoft Graph cannot identify the account.** An Azure v2.0 access token is
addressed to exactly one resource, and ours is addressed to `outlook.office.com`
for IMAP, so `graph.microsoft.com/v1.0/me` answers 401. The Outlook REST API
that would have accepted it was retired in 2022. The address is read from the
`id_token` instead, which comes back in the same token response.

**No migration was needed.** `provider_account_id`, `scopes` and
`token_expires_at` already existed from `20260815000100_extend_mail_accounts`,
and auth modes are seed data.

## 1. Flow

```
SPA                     API                         Provider
 |  GET  _authorize      |                              |
 |---------------------->|  store state in Redis (10m)  |
 |  {redirect_url}       |                              |
 |<----------------------|                              |
 |  browser redirect ------------------------------------>|  consent screen
 |                       |  GET _callback?code&state     |
 |                       |<----------------------------- |
 |                       |  exchange code -> tokens ---->|
 |                       |  encrypt + store              |
 |                       |  redirect back to the SPA     |
```

- [x] `OAuthStateCache` in `internal/gateway/cache/` — `state → {user_id, provider, account_id}`, TTL from `REDIS_TTL_OAUTH_STATE`
- [x] State is single-use — `GETDEL`, not read-then-delete, so simultaneous replays cannot both spend the code
- [x] Callback redirects to the SPA, it does not render JSON
- [x] `APP_BASE_URL` is the redirect target; the provider redirects to the new `OAUTH_CALLBACK_BASE_URL`, which is this API

## 2. Package

```
internal/gateway/oauth/
    oauth.go      Config, Tokens, Identity, Client, Registry, id_token reading
    google.go     Google endpoints + scopes + userinfo
    microsoft.go  Microsoft endpoints + scopes + id_token identity
    yandex.go     Yandex endpoints + scopes + passport identity
    stub.go       a real client pointed at a server we control, for tests
```

- [x] Added `golang.org/x/oauth2`
- [x] `Client` implementations for Google, Microsoft **and Yandex**
- [x] Config from env: `OAUTH_GOOGLE_CLIENT_ID`, `..._SECRET`, same for Microsoft and Yandex

Yandex was not in the original list. It is here because registering a Yandex
OAuth app takes minutes and needs no review, which makes it the only provider
this flow can be proved against before Google's assessment clears. It cost one
`Config` literal.

`Client` gained `Identify` beyond the interface sketched here: the consent
screen decides which mailbox was connected, not our form, so the callback has to
ask who the tokens belong to.

An unconfigured provider registers **nothing**, which is what keeps the `501` at
`_authorize` honest instead of sending a user to a screen that would reject them.

## 3. Token storage

Tokens go in the **existing encrypted `credentials` blob**. `token_expires_at`
and `scopes` are real columns because they are queried, and are not secret.

- [x] `MailAccountCredentials.AccessToken` / `RefreshToken` populated on connect
- [x] `token_expires_at`, `scopes`, `provider_account_id` populated on connect
- [x] Merge-on-update, never rebuild — see the bug in [04-decisions.md](04-decisions.md)
- [x] An empty refresh token never overwrites a stored one. Google omits it on
      every consent after the first, so rebuilding would strip the account's
      only way to renew and the failure would surface an hour later looking
      like a revoked grant

## 4. Refresh

- [x] Lazily, five minutes before expiry, inside `MailResolver.Resolve` rather
      than in a method each caller opts into — there are four ways into a
      mailbox and one forgotten call site is a mailbox that dies after an hour
- [x] Redis lock per account so concurrent workers refresh once; the losers wait
      briefly for the winner's write rather than duplicating the exchange
- [x] The renewal is committed on the resolver's own handle, **not** the
      caller's transaction. Providers retire the old refresh token when they
      issue a new one, so a renewal rolled back with the caller's work leaves
      the mailbox holding a credential that no longer exists
- [x] `invalid_grant` parks the account: `status = error`, `last_error`
      explaining the mailbox must be reconnected, no retry
- [x] `POST /api/mail-accounts/:id/_reauthorize` restarts consent for a mailbox
      that already exists. It fills `OAuthState.AccountID`, and `connect` reads
      it: the row is found by id, and the address the provider returns becomes
      a check rather than a lookup key. A consent screen that comes back naming
      a **different** mailbox is refused with `oauth_error=mismatch` and writes
      nothing — that row owns watchers, matches and a sync cursor built from one
      mailbox, and repointing it silently would carry all of it across. The
      provider comes off the account, not the caller, so one mailbox cannot be
      re-consented through another's flow. A mailbox still on an app password is
      accepted where its provider supports OAuth, since consent is how it stops
      needing one
- [x] The credential re-check job surfaces a parked account in the dashboard

## 5. XOAUTH2 over IMAP

- [x] `SASL XOAUTH2` client — hand-written, see the correction above
- [x] `connect()` picks LOGIN vs XOAUTH2 from `auth_mode`, and names a missing
      access token instead of sending `auth=Bearer ` and getting back a generic
      authentication failure
- [x] The token is refreshed before dialling, by the resolver
- [x] `gmail`, `outlook` and `yandex` seed rows carry `xoauth2` — the mode
      constant the code branches on. They previously said `oauth2`, which
      nothing matched

**Not verified against a live server.** GreenMail 2.1.0 advertises no SASL
mechanisms at all, so the handshake cannot complete against the dev stack. What
is verified against it is that `connect()` takes the token branch — the failure
is a rejected `AUTHENTICATE`, not a rejected `LOGIN`. The mechanism's own bytes
are covered by unit tests.

## 6. Google verification — start early

- [ ] Register the app, configure the consent screen
- [ ] Request `https://mail.google.com/`, **not** `gmail.readonly`
- [ ] Restricted scope → security assessment required
- [ ] Until verified, only test users can connect (fine for development)
- [x] The app-password fallback is what `_authorize` names in its `501`, so a
      user is never fully blocked

`gmail` and `outlook` are still seeded `enabled: false`, so `_authorize` answers
`400 … is not available yet` for them regardless of credentials. Enabling them
is a one-line seed edit plus `make seed`, and should happen at the same time as
the client id and secret land.

## What to do first when an app exists

Yandex, because it is minutes rather than weeks:

1. Register at `oauth.yandex.ru`, redirect URI
   `<OAUTH_CALLBACK_BASE_URL>/api/mail-accounts/oauth/yandex/_callback`
2. Set `OAUTH_YANDEX_CLIENT_ID` / `_SECRET`
3. Connect a real mailbox, confirm it syncs with no app password

That is the run that lets this file's status line change.
