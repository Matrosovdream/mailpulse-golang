-- Connection details move into a provider-specific jsonb blob, so an API
-- provider is not forced to carry IMAP host/port columns it has no use for.
alter table mail_accounts
    add column settings            jsonb        not null default '{}'::jsonb,
    add column auth_mode           varchar(20)  not null default 'password',
    add column provider_account_id varchar(255) null,
    add column scopes              text         null,
    add column token_expires_at    bigint       null,
    add column last_push_at        bigint       null;

-- carry the existing IMAP connections across before the columns are dropped
update mail_accounts
set settings = jsonb_strip_nulls(jsonb_build_object(
        'host', imap_host,
        'port', imap_port,
        'use_tls', imap_use_tls
    ))
where provider = 'imap';

-- app_password and password behave identically to the IMAP client; the
-- distinction exists so the UI can tell the user which one to create
update mail_accounts set auth_mode = auth_type;

alter table mail_accounts
    add constraint ck_mail_accounts_auth_mode check (auth_mode in
        ('password', 'app_password', 'oauth2', 'xoauth2'));

-- proactive refresh needs to find expiring grants cheaply
create index idx_mail_accounts_token_expiry
    on mail_accounts (token_expires_at) where token_expires_at is not null;
