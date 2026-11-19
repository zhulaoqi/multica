-- Keeps the chat message that created a dispatch so completion, failure and
-- progress notices can reply inside that conversation's topic.
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS origin_message_id text;
