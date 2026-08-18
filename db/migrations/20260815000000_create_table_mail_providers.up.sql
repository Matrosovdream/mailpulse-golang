-- Providers become data rather than a check constraint, so adding Fastmail or
-- Zoho is a seed row instead of a migration plus a code change.
--
-- slug is what a user picks and what mail_accounts references.
-- kind is which Provider implementation in internal/gateway/mail handles it,
-- so many slugs (yandex, mailru, fastmail) can share one client.
create table mail_providers
(
    slug            varchar(40)  not null,
    label           varchar(100) not null,
    kind            varchar(20)  not null,
    auth_modes      varchar(160) not null,
    default_host    varchar(255) null,
    default_port    int          null,
    default_use_tls boolean      not null default true,
    help_url        text         null,
    enabled         boolean      not null default true,
    position        int          not null default 0,
    created_at      bigint       not null,
    updated_at      bigint       not null,
    primary key (slug),
    constraint ck_mail_providers_kind check (kind in
        ('imap', 'gmail_api', 'graph_api', 'inbound_relay'))
);

create index idx_mail_providers_enabled on mail_providers (position) where enabled = true;

insert into mail_providers
    (slug, label, kind, auth_modes, default_host, default_port, default_use_tls, help_url, enabled, position, created_at, updated_at)
values
    ('imap', 'Generic IMAP', 'imap', 'password,app_password',
     null, 993, true, null, true, 0,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    ('yandex', 'Yandex Mail', 'imap', 'app_password',
     'imap.yandex.com', 993, true,
     'https://yandex.com/support/mail/mail-clients/others.html', true, 1,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    ('mailru', 'Mail.ru', 'imap', 'app_password',
     'imap.mail.ru', 993, true,
     'https://help.mail.ru/mail/mailer/popsmtp', true, 2,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    ('fastmail', 'Fastmail', 'imap', 'app_password',
     'imap.fastmail.com', 993, true,
     'https://www.fastmail.help/hc/en-us/articles/1500000278342', true, 3,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    ('zoho', 'Zoho Mail', 'imap', 'app_password',
     'imap.zoho.com', 993, true, null, true, 4,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    -- disabled until the app-password path is documented; Gmail also needs
    -- OAuth before it is worth offering as a first-class choice
    ('gmail', 'Gmail', 'imap', 'app_password,oauth2',
     'imap.gmail.com', 993, true,
     'https://support.google.com/mail/answer/185833', false, 5,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint),

    ('outlook', 'Outlook', 'imap', 'app_password,oauth2',
     'outlook.office365.com', 993, true, null, false, 6,
     (extract(epoch from now()) * 1000)::bigint, (extract(epoch from now()) * 1000)::bigint);
