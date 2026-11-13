CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_unit_external_id_uidx
    ON org_unit (workspace_id, external_id)
    WHERE external_id IS NOT NULL;
