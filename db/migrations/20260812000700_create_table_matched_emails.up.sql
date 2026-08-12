create table matched_emails
(
    id              varchar(36)  not null,
    user_id         varchar(36)  not null,
    watcher_id      varchar(36)  not null,
    mail_account_id varchar(36)  not null,
    message_id      varchar(512) not null,
    provider_uid    varchar(128) not null,
    subject         text         null,
    from_address    varchar(320) null,
    from_name       varchar(255) null,
    to_addresses    text         null,
    snippet         text         null,
    has_attachment  boolean      not null default false,
    size_bytes      int          not null default 0,
    received_at     bigint       not null,
    matched_at      bigint       not null,
    matched_filters jsonb        not null default '[]'::jsonb,
    created_at      bigint       not null,
    updated_at      bigint       not null,
    primary key (id),
    -- the dedupe guard: a re-read mailbox cannot fire the same watcher twice
    constraint uq_matched_emails_watcher_message unique (watcher_id, message_id),
    constraint fk_matched_emails_user foreign key (user_id) references users (id) on delete cascade,
    constraint fk_matched_emails_watcher foreign key (watcher_id) references watchers (id) on delete cascade,
    constraint fk_matched_emails_mail_account foreign key (mail_account_id) references mail_accounts (id) on delete cascade
);

create index idx_matched_emails_user_time on matched_emails (user_id, matched_at desc);

create index idx_matched_emails_watcher_time on matched_emails (watcher_id, matched_at desc);
