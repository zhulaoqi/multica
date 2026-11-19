-- The requester is told once when a dispatch's squad-leader run fails for
-- good (migration 660).
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS failure_notified_at timestamptz;

INSERT INTO schema_migrations (version) VALUES ('017_dispatch_failure_notice');
