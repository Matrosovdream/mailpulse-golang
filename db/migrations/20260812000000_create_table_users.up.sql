create table users
(
    id                varchar(36)  not null,
    email             varchar(320) not null,
    name              varchar(100) not null,
    password          varchar(100) not null,
    status            varchar(20)  not null default 'active',
    timezone          varchar(64)  not null default 'UTC',
    email_verified_at bigint       null,
    created_at        bigint       not null,
    updated_at        bigint       not null,
    primary key (id),
    constraint uq_users_email unique (email),
    constraint ck_users_status check (status in ('active', 'suspended'))
);
