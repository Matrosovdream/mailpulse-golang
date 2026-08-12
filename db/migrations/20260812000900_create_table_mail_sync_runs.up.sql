create table mail_sync_runs
(
    id               varchar(36) not null,
    mail_account_id  varchar(36) not null,
    status           varchar(20) not null default 'running',
    started_at       bigint      not null,
    finished_at      bigint      null,
    messages_fetched int         not null default 0,
    matches_created  int         not null default 0,
    error            text        null,
    created_at       bigint      not null,
    updated_at       bigint      not null,
    primary key (id),
    constraint fk_mail_sync_runs_mail_account foreign key (mail_account_id) references mail_accounts (id) on delete cascade,
    constraint ck_mail_sync_runs_status check (status in ('running', 'ok', 'error'))
);

create index idx_mail_sync_runs_account_time on mail_sync_runs (mail_account_id, started_at desc);
