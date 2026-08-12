create table mail_accounts
(
    id                    varchar(36)  not null,
    user_id               varchar(36)  not null,
    provider              varchar(20)  not null,
    email_address         varchar(320) not null,
    display_name          varchar(150) null,
    auth_type             varchar(20)  not null,
    credentials           text         not null,
    imap_host             varchar(255) null,
    imap_port             int          null,
    imap_use_tls          boolean      not null default true,
    status                varchar(20)  not null default 'pending',
    last_verified_at      bigint       null,
    last_error            text         null,
    sync_state            jsonb        not null default '{}'::jsonb,
    last_synced_at        bigint       null,
    poll_interval_seconds int          not null default 120,
    next_poll_at          bigint       not null,
    created_at            bigint       not null,
    updated_at            bigint       not null,
    primary key (id),
    constraint uq_mail_accounts_user_email unique (user_id, email_address),
    constraint fk_mail_accounts_user foreign key (user_id) references users (id) on delete cascade,
    constraint ck_mail_accounts_provider check (provider in ('gmail', 'outlook', 'imap')),
    constraint ck_mail_accounts_auth_type check (auth_type in ('oauth2', 'password')),
    constraint ck_mail_accounts_status check (status in ('pending', 'verified', 'error', 'disabled')),
    constraint ck_mail_accounts_imap check (
        provider <> 'imap' or (imap_host is not null and imap_port is not null))
);

create index idx_mail_accounts_user on mail_accounts (user_id);

-- the sync worker's work queue: claim verified accounts whose poll is due
create index idx_mail_accounts_due on mail_accounts (next_poll_at) where status = 'verified';
