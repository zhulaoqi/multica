-- Where a dispatch came from, so the requester is told when the department
-- delivers. One row per dispatched root issue; notified_at makes the
-- completion notice idempotent across duplicate status events.
CREATE TABLE IF NOT EXISTS dispatch_origin (
  issue_id uuid PRIMARY KEY,
  workspace_id uuid NOT NULL,
  requester_id uuid NOT NULL,
  target_unit_id uuid NOT NULL,
  channel_type text NOT NULL DEFAULT 'app',
  installation_id uuid,
  chat_id text,
  thread_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  notified_at timestamptz
);
