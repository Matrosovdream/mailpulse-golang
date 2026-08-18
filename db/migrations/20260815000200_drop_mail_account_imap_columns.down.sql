alter table mail_accounts
    drop constraint if exists fk_mail_accounts_provider;

alter table mail_accounts
    add column imap_host    varchar(255) null,
    add column imap_port    int          null,
    add column imap_use_tls boolean      not null default true,
    add column auth_type    varchar(20)  not null default 'password';

update mail_accounts
set imap_host    = settings ->> 'host',
    imap_port    = nullif(settings ->> 'port', '')::int,
    imap_use_tls = coalesce((settings ->> 'use_tls')::boolean, true),
    auth_type    = case when auth_mode in ('password', 'app_password') then 'password' else 'oauth2' end;

-- Rolling back reinstates an enum that predates the provider table, so any
-- account on a provider that enum never had (yandex, fastmail, …) has to be
-- folded back onto the kind that actually served it. Every one of those is an
-- IMAP client, and the host it was using is preserved in imap_host above, so
-- the connection still works after the rollback.
update mail_accounts
set provider = 'imap'
where provider not in ('gmail', 'outlook', 'imap');

alter table mail_accounts
    add constraint ck_mail_accounts_provider check (provider in ('gmail', 'outlook', 'imap')),
    add constraint ck_mail_accounts_auth_type check (auth_type in ('oauth2', 'password')),
    add constraint ck_mail_accounts_imap check (
        provider <> 'imap' or (imap_host is not null and imap_port is not null));
