create table roles
(
    id          varchar(36)  not null,
    slug        varchar(50)  not null,
    name        varchar(100) not null,
    description text         null,
    created_at  bigint       not null,
    updated_at  bigint       not null,
    primary key (id),
    constraint uq_roles_slug unique (slug)
);

create table user_roles
(
    user_id    varchar(36) not null,
    role_id    varchar(36) not null,
    created_at bigint      not null,
    primary key (user_id, role_id),
    constraint fk_user_roles_user foreign key (user_id) references users (id) on delete cascade,
    constraint fk_user_roles_role foreign key (role_id) references roles (id) on delete cascade
);

-- lets "who holds this role" stay an index lookup for the admin screens
create index idx_user_roles_role on user_roles (role_id);

-- fixed ids so application code and later migrations can reference roles directly
insert into roles (id, slug, name, description, created_at, updated_at)
values ('00000000-0000-0000-0000-000000000001',
        'user',
        'User',
        'Standard account. Sees and manages only their own mail accounts, watchers and notifiers.',
        (extract(epoch from now()) * 1000)::bigint,
        (extract(epoch from now()) * 1000)::bigint),
       ('00000000-0000-0000-0000-000000000002',
        'superadmin',
        'Super admin',
        'Read access across every account, plus user management and impersonation.',
        (extract(epoch from now()) * 1000)::bigint,
        (extract(epoch from now()) * 1000)::bigint);
