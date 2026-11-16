-- Idempotency key for in-app dispatches. The workbench retries a send after a
-- dropped response; this key lets the retry resolve to the first dispatch
-- instead of creating a second root issue. Chat dispatches leave it NULL.
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS client_request_id text;
