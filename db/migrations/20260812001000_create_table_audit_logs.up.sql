create table audit_logs
(
    id                   varchar(36)  not null,
    actor_user_id        varchar(36)  null,
    impersonated_user_id varchar(36)  null,
    action               varchar(80)  not null,
    entity_type          varchar(50)  not null,
    entity_id            varchar(36)  null,
    metadata             jsonb        not null default '{}'::jsonb,
    ip                   varchar(45)  null,
    user_agent           varchar(255) null,
    created_at           bigint       not null,
    primary key (id),
    -- set null rather than cascade: deleting a user must not erase the record
    -- of what they did
    constraint fk_audit_logs_actor foreign key (actor_user_id) references users (id) on delete set null,
    constraint fk_audit_logs_impersonated foreign key (impersonated_user_id) references users (id) on delete set null
);

create index idx_audit_logs_actor_time on audit_logs (actor_user_id, created_at desc);

create index idx_audit_logs_entity on audit_logs (entity_type, entity_id);

create index idx_audit_logs_time on audit_logs (created_at desc);
