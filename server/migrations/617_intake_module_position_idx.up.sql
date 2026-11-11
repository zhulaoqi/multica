CREATE INDEX CONCURRENTLY IF NOT EXISTS intake_module_position_idx
    ON intake_module (workspace_id, project_id, position);
