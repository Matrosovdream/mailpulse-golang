-- Second step, deliberately separate from the backfill: the application reads
-- settings before these columns disappear, so the two migrations can be
-- deployed apart if a rollback is ever needed between them.
alter table mail_accounts
    drop constraint if exists ck_mail_accounts_imap,
    drop constraint if exists ck_mail_accounts_auth_type,
    drop constraint if exists ck_mail_accounts_provider;

alter table mail_accounts
    drop column imap_host,
    drop column imap_port,
    drop column imap_use_tls,
    drop column auth_type;

-- provider is now a reference, not an enum: adding an integration is a row
alter table mail_accounts
    add constraint fk_mail_accounts_provider foreign key (provider)
        references mail_providers (slug) on delete restrict;
