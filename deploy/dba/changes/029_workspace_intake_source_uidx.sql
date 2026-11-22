-- One row per source per project (migration 672).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_intake_source_uidx
    ON workspace_intake_source (workspace_id, source_type, source_id);

INSERT INTO schema_migrations (version) VALUES ('029_workspace_intake_source_uidx');
