-- The requester is told once when a dispatch's squad-leader run fails for
-- good. The timestamp is the idempotency claim for that notice.
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS failure_notified_at timestamptz;
