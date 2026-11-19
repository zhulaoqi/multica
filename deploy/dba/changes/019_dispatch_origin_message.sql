-- Keeps the chat message that created a dispatch so completion, failure and
-- progress notices can reply inside that conversation's topic (migration 662).
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS origin_message_id text;

INSERT INTO schema_migrations (version) VALUES ('019_dispatch_origin_message');
