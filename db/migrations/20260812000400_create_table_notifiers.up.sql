create table notifiers
(
    id                      varchar(36)  not null,
    user_id                 varchar(36)  not null,
    type                    varchar(30)  not null,
    name                    varchar(100) not null,
    config                  jsonb        not null default '{}'::jsonb,
    secrets                 text         null,
    status                  varchar(20)  not null default 'pending',
    verification_code       varchar(20)  null,
    verification_expires_at bigint       null,
    verified_at             bigint       null,
    last_error              text         null,
    is_default              boolean      not null default false,
    created_at              bigint       not null,
    updated_at              bigint       not null,
    primary key (id),
    constraint uq_notifiers_user_name unique (user_id, name),
    constraint fk_notifiers_user foreign key (user_id) references users (id) on delete cascade,
    constraint ck_notifiers_type check (type in
        ('telegram', 'sms', 'email', 'webhook', 'slack', 'discord', 'push')),
    constraint ck_notifiers_status check (status in ('pending', 'verified', 'error', 'disabled'))
);

create index idx_notifiers_user_status on notifiers (user_id, status);
