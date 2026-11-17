-- Workbench history: one requester's dispatches, newest first (migration 649).
CREATE INDEX CONCURRENTLY IF NOT EXISTS dispatch_origin_list_idx
    ON dispatch_origin (workspace_id, requester_id, created_at DESC);

INSERT INTO schema_migrations (version) VALUES ('006_dispatch_list_idx');
