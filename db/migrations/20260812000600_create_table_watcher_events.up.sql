create table watcher_events
(
    id                      varchar(36)  not null,
    watcher_id              varchar(36)  not null,
    type                    varchar(40)  not null,
    config                  jsonb        not null default '{}'::jsonb,
    position                int          not null default 0,
    enabled                 boolean      not null default true,
    run_mode                varchar(20)  not null default 'immediate',
    delay_seconds           int          not null default 0,
    repeat_interval_seconds int          null,
    repeat_max              int          null,
    repeat_until            bigint       null,
    cron_expression         varchar(120) null,
    stop_on_ack             boolean      not null default false,
    created_at              bigint       not null,
    updated_at              bigint       not null,
    primary key (id),
    constraint fk_watcher_events_watcher foreign key (watcher_id) references watchers (id) on delete cascade,
    constraint ck_watcher_events_run_mode check (run_mode in ('immediate', 'delayed', 'recurring')),
    -- a recurring event without a cadence would schedule one occurrence and stall
    constraint ck_watcher_events_recurring check (
        run_mode <> 'recurring'
            or repeat_interval_seconds is not null
            or cron_expression is not null),
    constraint ck_watcher_events_repeat_max check (repeat_max is null or repeat_max > 0)
);

create index idx_watcher_events_watcher on watcher_events (watcher_id, position);

create table watcher_event_notifiers
(
    watcher_event_id varchar(36) not null,
    notifier_id      varchar(36) not null,
    position         int         not null default 0,
    created_at       bigint      not null,
    primary key (watcher_event_id, notifier_id),
    constraint fk_wen_watcher_event foreign key (watcher_event_id) references watcher_events (id) on delete cascade,
    -- restrict, so deleting a notifier still in use returns a conflict the UI can explain
    constraint fk_wen_notifier foreign key (notifier_id) references notifiers (id) on delete restrict
);

-- answers "which events still use this notifier?" before a delete
create index idx_wen_notifier on watcher_event_notifiers (notifier_id);
