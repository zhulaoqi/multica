CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_intake_source_uidx
    ON workspace_intake_source (workspace_id, source_type, source_id);
