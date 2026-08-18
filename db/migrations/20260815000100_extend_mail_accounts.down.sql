drop index if exists idx_mail_accounts_token_expiry;

alter table mail_accounts
    drop constraint if exists ck_mail_accounts_auth_mode;

alter table mail_accounts
    drop column last_push_at,
    drop column token_expires_at,
    drop column scopes,
    drop column provider_account_id,
    drop column auth_mode,
    drop column settings;
