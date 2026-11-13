CREATE INDEX CONCURRENTLY IF NOT EXISTS org_sync_run_workspace_idx
    ON org_sync_run (workspace_id, created_at DESC);
