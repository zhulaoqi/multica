-- The requester hears once when the department starts working on a chat
-- dispatch, before the completion or failure notice (migration 661).
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS started_notified_at timestamptz;

INSERT INTO schema_migrations (version) VALUES ('018_dispatch_start_notice');
