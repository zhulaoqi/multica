-- The requester hears once when the department starts working on a chat
-- dispatch, before the completion or failure notice.
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS started_notified_at timestamptz;
