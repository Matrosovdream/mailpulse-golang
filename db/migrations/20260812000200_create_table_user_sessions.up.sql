create table user_sessions
(
    id           varchar(36)  not null,
    user_id      varchar(36)  not null,
    token_hash   varchar(64)  not null,
    user_agent   varchar(255) null,
    ip           varchar(45)  null,
    expires_at   bigint       not null,
    revoked_at   bigint       null,
    last_used_at bigint       not null,
    created_at   bigint       not null,
    updated_at   bigint       not null,
    primary key (id),
    constraint uq_user_sessions_token unique (token_hash),
    constraint fk_user_sessions_user foreign key (user_id) references users (id) on delete cascade
);

create index idx_user_sessions_user on user_sessions (user_id);

-- drives the "active sessions" list and the expiry sweep
create index idx_user_sessions_expiry on user_sessions (expires_at) where revoked_at is null;
