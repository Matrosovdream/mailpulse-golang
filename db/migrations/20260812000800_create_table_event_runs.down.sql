drop index if exists idx_deliveries_notifier_time;

drop index if exists idx_deliveries_event_run;

drop table notification_deliveries;

drop index if exists idx_event_runs_user_time;

drop index if exists idx_event_runs_matched_email;

drop index if exists idx_event_runs_due;

drop table event_runs;
