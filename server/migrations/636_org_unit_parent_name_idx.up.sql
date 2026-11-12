CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_unit_parent_name_idx
    ON org_unit (workspace_id, parent_id, name) NULLS NOT DISTINCT;
