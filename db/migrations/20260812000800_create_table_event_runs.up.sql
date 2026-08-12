create table event_runs
(
    id               varchar(36) not null,
    user_id          varchar(36) not null,
    matched_email_id varchar(36) not null,
    watcher_event_id varchar(36) not null,
    occurrence       int         not null default 1,
    status           varchar(20) not null default 'pending',
    scheduled_at     bigint      not null,
    started_at       bigint      null,
    finished_at      bigint      null,
    attempt          int         not null default 0,
    max_attempts     int         not null default 3,
    next_retry_at    bigint      null,
    config_snapshot  jsonb       not null default '{}'::jsonb,
    result           jsonb       null,
    error            text        null,
    acknowledged_at  bigint      null,
    created_at       bigint      not null,
    updated_at       bigint      not null,
    primary key (id),
    -- makes scheduling idempotent: a retried pass cannot create occurrence 2 twice
    constraint uq_event_runs_occurrence unique (matched_email_id, watcher_event_id, occurrence),
    constraint fk_event_runs_user foreign key (user_id) references users (id) on delete cascade,
    constraint fk_event_runs_matched_email foreign key (matched_email_id) references matched_emails (id) on delete cascade,
    constraint fk_event_runs_watcher_event foreign key (watcher_event_id) references watcher_events (id) on delete cascade,
    constraint ck_event_runs_status check (status in
        ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
    constraint ck_event_runs_occurrence check (occurrence > 0)
);

-- the dispatcher's claim index:
--   select ... where status = 'pending' and scheduled_at <= $1
--   order by scheduled_at limit 50 for update skip locked
create index idx_event_runs_due on event_runs (scheduled_at) where status = 'pending';

create index idx_event_runs_matched_email on event_runs (matched_email_id);

create index idx_event_runs_user_time on event_runs (user_id, created_at desc);

create table notification_deliveries
(
    id                  varchar(36)  not null,
    event_run_id        varchar(36)  not null,
    notifier_id         varchar(36)  null,
    channel_type        varchar(30)  not null,
    status              varchar(20)  not null default 'pending',
    rendered_message    text         null,
    provider_message_id varchar(255) null,
    error               text         null,
    sent_at             bigint       null,
    created_at          bigint       not null,
    updated_at          bigint       not null,
    primary key (id),
    constraint fk_deliveries_event_run foreign key (event_run_id) references event_runs (id) on delete cascade,
    -- set null plus the copied channel_type keeps delivery history readable
    -- after the notifier itself is deleted
    constraint fk_deliveries_notifier foreign key (notifier_id) references notifiers (id) on delete set null,
    constraint ck_deliveries_status check (status in ('pending', 'sent', 'failed'))
);

create index idx_deliveries_event_run on notification_deliveries (event_run_id);

create index idx_deliveries_notifier_time on notification_deliveries (notifier_id, created_at desc);
