CREATE INDEX CONCURRENTLY IF NOT EXISTS squad_org_unit_idx
    ON squad (workspace_id, org_unit_id);
