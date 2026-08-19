drop index if exists idx_user_sessions_impersonated_by;

alter table user_sessions
    drop constraint if exists fk_user_sessions_impersonator;

alter table user_sessions
    drop column impersonated_by;
