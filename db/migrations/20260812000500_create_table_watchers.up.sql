create table watchers
(
    id               varchar(36)  not null,
    user_id          varchar(36)  not null,
    mail_account_id  varchar(36)  not null,
    name             varchar(150) not null,
    description      text         null,
    status           varchar(20)  not null default 'active',
    match_mode       varchar(10)  not null default 'all',
    folder           varchar(255) not null default 'INBOX',
    watch_from       bigint       null,
    cooldown_seconds int          not null default 0,
    last_matched_at  bigint       null,
    match_count      bigint       not null default 0,
    archived_at      bigint       null,
    created_at       bigint       not null,
    updated_at       bigint       not null,
    primary key (id),
    constraint fk_watchers_user foreign key (user_id) references users (id) on delete cascade,
    -- restrict, so deleting a mailbox with live watchers fails loudly instead of
    -- silently destroying the rules built on top of it
    constraint fk_watchers_mail_account foreign key (mail_account_id) references mail_accounts (id) on delete restrict,
    constraint ck_watchers_status check (status in ('active', 'paused', 'archived')),
    constraint ck_watchers_match_mode check (match_mode in ('all', 'any'))
);

create index idx_watchers_user_status on watchers (user_id, status);

-- the match engine loads every active watcher for the account it just synced
create index idx_watchers_account_active on watchers (mail_account_id) where status = 'active';

create table watcher_filters
(
    id             varchar(36)  not null,
    watcher_id     varchar(36)  not null,
    field          varchar(40)  not null,
    header_name    varchar(100) null,
    operator       varchar(20)  not null,
    value          text         not null,
    case_sensitive boolean      not null default false,
    position       int          not null default 0,
    created_at     bigint       not null,
    updated_at     bigint       not null,
    primary key (id),
    constraint fk_watcher_filters_watcher foreign key (watcher_id) references watchers (id) on delete cascade,
    constraint ck_watcher_filters_field check (field in
        ('subject', 'from', 'to', 'cc', 'body', 'header', 'attachment_name', 'has_attachment', 'size')),
    constraint ck_watcher_filters_operator check (operator in
        ('contains', 'not_contains', 'equals', 'starts_with', 'ends_with', 'regex', 'gt', 'lt', 'exists')),
    constraint ck_watcher_filters_header check (field <> 'header' or header_name is not null)
);

create index idx_watcher_filters_watcher on watcher_filters (watcher_id, position);
