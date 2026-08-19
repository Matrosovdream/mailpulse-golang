-- An impersonated session has to be recognisable at Verify time, because that
-- is the only place the raw token is resolved to an identity. Without this the
-- session is indistinguishable from the target user logging in themselves, and
-- every action taken on it is attributed to them rather than to the admin.
alter table user_sessions
    add column impersonated_by varchar(36) null;

alter table user_sessions
    add constraint fk_user_sessions_impersonator foreign key (impersonated_by)
        references users (id) on delete cascade;

-- "which sessions is someone currently impersonating through" is the question
-- an incident review asks; without the index it is a full scan
create index idx_user_sessions_impersonated_by
    on user_sessions (impersonated_by) where impersonated_by is not null;
