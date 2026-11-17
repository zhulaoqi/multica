-- Idempotency key for in-app dispatches (migration 647).
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS client_request_id text;

INSERT INTO schema_migrations (version) VALUES ('004_dispatch_client_request_id');
