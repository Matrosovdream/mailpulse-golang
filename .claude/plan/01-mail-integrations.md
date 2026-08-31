# 01 — Mail integrations

**Status:** steps 1–3 of the order below are done and verified. OAuth, push
and the API-based providers remain.

The goal is that adding "Fastmail" or "Zoho" is a seed row, and adding "Gmail
API" is one new package — with no change to routes, controllers, the matcher,
or the SPA's connect form.

---

## 1. The two axes

These are independent and are currently conflated in `mail_accounts.provider`
and `auth_type`. Keeping them separate is the whole design.

**Transport / protocol — how mail is read:**

| Kind | Examples | Cursor | Notes |
|------|----------|--------|-------|
| `imap` | any host, Yandex, Mail.ru, Fastmail, Zoho | `UIDVALIDITY` + `UID` | done |
| `gmail_api` | Gmail, Google Workspace | `historyId` | labels, not folders |
| `graph_api` | Outlook.com, Microsoft 365 | delta token | folders + webhooks |
| `inbound_relay` | Mailgun / SendGrid inbound parse | none (push only) | mail arrives at *us* |
| `ews` | legacy on-prem Exchange | sync state | only if a customer needs it |

**Auth mode — how we prove we may read it:**

| Mode | Used by | Stored |
|------|---------|--------|
| `password` | plain IMAP, self-hosted | password |
| `app_password` | Yandex, Gmail (IMAP), Fastmail | app-specific password |
| `oauth2` | Gmail API, Graph API | refresh + access token, expiry, scopes |
| `xoauth2` | Yandex/Gmail **over IMAP** with an OAuth token | same tokens, sent as an IMAP SASL mech |

`xoauth2` is the awkward one worth noting early: it is IMAP transport with
OAuth credentials. It proves the two axes are genuinely orthogonal, and it is
why `auth_type` must not stay a two-value enum.

- [x] Agree the axis split before writing any provider code

---

## 2. Interface shape

Current contract in `internal/gateway/mail/provider.go`:

```go
type Provider interface {
    Type() string
    Verify(ctx, Account) ([]Folder, error)
    Folders(ctx, Account) ([]Folder, error)
    Fetch(ctx, Account, FetchRequest) (FetchResult, error)
}
```

It works, but every provider is forced to implement everything, and the SPA
cannot discover what a provider needs. Proposed:

```go
// Describe replaces Type(): it carries everything the API and SPA need to
// render a connect form and reason about the connection.
type Descriptor struct {
    Slug         string          // "imap", "gmail", "yandex"
    Label        string          // "Generic IMAP", "Gmail"
    Kind         string          // imap | gmail_api | graph_api | inbound_relay
    AuthModes    []string        // password, app_password, oauth2, xoauth2
    Capabilities Capabilities
    ConfigSchema model.Schema    // same shape the notifier registry already serves
    Defaults     Defaults        // host/port/tls presets, help text, docs URL
}

type Capabilities struct {
    Folders      bool // can enumerate mailboxes
    Labels       bool // gmail-style labels rather than folders
    Push         bool // server can notify us instead of being polled
    Idle         bool // IMAP IDLE available
    ServerSearch bool // can push filtering server-side
}

type Provider interface {
    Describe() Descriptor
    Verify(ctx context.Context, a Account) ([]Folder, error)
    Fetch(ctx context.Context, a Account, r FetchRequest) (FetchResult, error)
}
```

Everything optional becomes a **separate interface, discovered by type
assertion** — the idiomatic Go way to add capability without forcing every
implementation to carry stubs:

```go
type FolderLister interface {
    Folders(ctx context.Context, a Account) ([]Folder, error)
}

type PushProvider interface {
    Subscribe(ctx context.Context, a Account, callback string) (Subscription, error)
    Renew(ctx context.Context, a Account, s Subscription) (Subscription, error)
    Unsubscribe(ctx context.Context, a Account, s Subscription) error
    ParseNotification(ctx context.Context, body []byte, headers map[string]string) (Notification, error)
}

type TokenRefresher interface {
    // called before Verify/Fetch when token_expires_at is near
    Refresh(ctx context.Context, a Account) (Credentials, error)
}
```

- [x] Add `Descriptor`, `Capabilities`, `Defaults`
- [x] Swap `Type()` for `Describe()`, update the registry
- [x] Move `Folders` to the optional `FolderLister`
- [x] Define `PushProvider`, `TokenRefresher` (no implementations yet)

### `Account` must stop being IMAP-shaped

`mail.Account` currently has `Host`, `Port`, `UseTLS` — meaningless for an API
provider. Replace the protocol-specific fields with an opaque blob each
provider decodes into its own struct:

```go
type Account struct {
    ID           string
    Provider     string
    EmailAddress string
    AuthMode     string
    Settings     json.RawMessage // provider-specific, from mail_accounts.settings
    Credentials  Credentials     // decrypted by the usecase, never by the gateway
}

type Credentials struct {
    Username     string
    Password     string
    AccessToken  string
    RefreshToken string
    ExpiresAt    int64
}
```

- [x] Introduce `Settings` + `Credentials`, migrate the IMAP provider onto them
- [x] Keep decryption in the usecase — the gateway never touches the cipher

---

## 3. Package layout

Parent holds the contract; children implement it; nothing in the parent imports
a child, so registration happens in `internal/config/app.go` and there are no
cycles.

```
internal/gateway/mail/
    provider.go        contract: Provider, Descriptor, Account, Message, Registry
    capability.go      Capabilities, optional interfaces
    registry.go        lookup + Descriptors() for the catalog endpoint
    imap/              package imapmail   — generic IMAP  (move existing code here)
    gmail/             package gmailmail  — Gmail API
    graph/             package graphmail  — Microsoft Graph
    relay/             package relaymail  — inbound webhook ingestion
    stub/              package stubmail   — dev fake (move existing code here)
internal/gateway/oauth/
    oauth.go           token exchange + refresh, per-provider client config
```

- [x] Create `mail/imap/`, move `imap_provider.go`, keep behaviour identical
- [x] Create `mail/stub/`, move `stub_provider.go`
- [x] Registry gains `Descriptors()` for the catalog endpoint

---

## 4. Database changes

### 4a. Providers become data, not a check constraint

`ck_mail_accounts_provider` means every new integration is a migration. Replace
it with a seeded reference table so adding Fastmail is one row.

```sql
-- 20260815000000_create_table_mail_providers
create table mail_providers
(
    slug            varchar(40)  not null,
    label           varchar(100) not null,
    kind            varchar(20)  not null,   -- imap | gmail_api | graph_api | inbound_relay
    auth_modes      varchar(160) not null,   -- csv: password,app_password,oauth2,xoauth2
    default_host    varchar(255) null,
    default_port    int          null,
    default_use_tls boolean      not null default true,
    help_url        text         null,
    enabled         boolean      not null default true,
    position        int          not null default 0,
    created_at      bigint       not null,
    updated_at      bigint       not null,
    primary key (slug)
);
```

Seed: `imap`, `gmail`, `outlook`, `yandex`, `mailru`, `fastmail`, `zoho`.
Yandex seeds as `kind=imap`, `default_host=imap.yandex.com`, `default_port=993`,
`auth_modes=app_password,xoauth2` — which is exactly the preset the connect
form needs.

- [x] Migration: create + seed `mail_providers`
- [x] Migration: drop `ck_mail_accounts_provider`, add FK `mail_accounts.provider → mail_providers.slug`
- [x] Registry validates that a code provider exists for each enabled row at boot

### 4b. `mail_accounts` columns

```sql
-- 20260815000100_extend_mail_accounts
alter table mail_accounts
    add column settings            jsonb        not null default '{}'::jsonb,
    add column auth_mode           varchar(20)  not null default 'password',
    add column provider_account_id varchar(255) null,
    add column scopes              text         null,
    add column token_expires_at    bigint       null,
    add column last_push_at        bigint       null;

create index idx_mail_accounts_token_expiry
    on mail_accounts (token_expires_at) where token_expires_at is not null;
```

`imap_host` / `imap_port` / `imap_use_tls` move into `settings` and the columns
are dropped in a **later** migration, once the IMAP provider reads from
`settings` — never in the same step.

- [x] Migration: add the columns above
- [x] Backfill `settings` from the existing imap_* columns
- [x] Widen `ck_mail_accounts_auth_type` to the four auth modes
- [x] Separate later migration: drop `imap_host`, `imap_port`, `imap_use_tls`

### 4c. Push subscriptions

Needed by Graph and Gmail Pub/Sub; both expire and must be renewed.

```sql
-- 20260815000200_create_table_mail_push_subscriptions
create table mail_push_subscriptions
(
    id                       varchar(36)  not null,
    mail_account_id          varchar(36)  not null,
    provider_subscription_id varchar(255) not null,
    resource                 varchar(255) null,
    client_secret            text         null,   -- encrypted; validates callbacks
    status                   varchar(20)  not null default 'active',
    expires_at               bigint       not null,
    last_renewed_at          bigint       null,
    created_at               bigint       not null,
    updated_at               bigint       not null,
    primary key (id),
    constraint fk_push_account foreign key (mail_account_id)
        references mail_accounts (id) on delete cascade
);

create index idx_push_renewal on mail_push_subscriptions (expires_at) where status = 'active';
```

- [ ] Migration: create `mail_push_subscriptions`
- [ ] Worker loop: renew anything expiring inside the next hour

### 4d. OAuth state

Short-lived CSRF state for authorize → callback. **No table** — Redis, same
pattern as `PasswordResetCache`.

- [x] `OAuthStateCache` in `internal/gateway/cache/` — built and wired
      (`config/app.go`, consumed by `mail_account_usecase`), single-use state
      covered by `test/integration/oauth_state_test.go`

---

## 5. Per-integration checklist

### Generic IMAP — done
- [x] Connect, implicit TLS and STARTTLS upgrade
- [x] `UIDVALIDITY` + `UID` cursor, `N:*` overlap guard
- [x] MIME parse: body, attachments, headers
- [x] RFC 2047 subjects, windows-1251 / koi8-r bodies, UTF-7 folder names
- [x] Read-only (`BODY.PEEK`, readonly SELECT)
- [ ] IMAP `IDLE` instead of polling (optional, big win on latency)
- [ ] Server-side `SEARCH` pushdown for cheap pre-filtering

### Yandex — works today via generic IMAP
- [x] Reachability + capabilities confirmed (`AUTH=PLAIN`, `AUTH=XOAUTH2`, `UIDPLUS`)
- [x] STARTTLS on 143 verified
- [x] Seed `mail_providers` row with the `imap.yandex.com:993` preset
- [ ] Docs: user must enable IMAP and create an **app password**
- [ ] `xoauth2` mode so users skip app passwords

### Gmail
- [ ] OAuth2 client + consent screen (needs Google verification — start early)
- [ ] `gmail_api` provider: `users.history.list` with `historyId` cursor
- [ ] Labels mapped onto the `Folder` shape
- [ ] Pub/Sub push (`users.watch`, renew every 7 days)
- [ ] Fallback: `app_password` over IMAP for users who won't do OAuth

### Outlook / Microsoft 365
- [ ] Azure app registration, delegated `Mail.Read`
- [ ] `graph_api` provider with delta tokens
- [ ] Graph webhook subscriptions (expire ~3 days, renewal loop required)

### Inbound relay (Mailgun / SendGrid)
- [ ] Inverted flow: mail is POSTed to us, no polling and no cursor
- [ ] Public signed webhook route, per-account secret
- [ ] Feeds the same matcher — the pipeline should not care how mail arrived

---

## 6. API surface

- [x] `GET /api/mail-provider-types` — the missing catalog endpoint. Serves
      `Descriptor`s so the connect form renders from the server, exactly like
      `/api/notifier-types` and `/api/event-types` already do. **This is the
      one inconsistency in the current design.**
- [x] `POST /api/mail-accounts` accepts `settings` + `auth_mode`
- [x] `POST /api/mail-accounts/:id/_reauthorize` for expired OAuth grants
      (see [02](02-oauth.md#4-refresh))
- [ ] `POST /api/webhooks/mail/:provider` for push notifications
- [ ] `_verify` and `_sync` become queued + polled rather than synchronous
      (both currently block for up to `MAIL_IMAP_TIMEOUT`)

---

## 7. Suggested order

1. `mail_providers` table + `GET /api/mail-provider-types` + Yandex preset —
   small, unblocks the SPA, no behaviour change.
2. `Descriptor` / `Capabilities` / optional interfaces; move IMAP and stub into
   subpackages. Pure refactor, covered by the existing smoke suite.
3. `settings` + `auth_mode` columns, IMAP reads from `settings`, drop the
   `imap_*` columns in a follow-up migration.
4. OAuth plumbing ([02-oauth.md](02-oauth.md)) — then Gmail, then Graph.
5. Push subscriptions and the renewal loop.
6. Inbound relay.

Step 1 and 2 are worth doing before any second provider exists: they are much
cheaper now than after there are three implementations to migrate.
