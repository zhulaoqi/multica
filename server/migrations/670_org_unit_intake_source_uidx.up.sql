CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_unit_intake_source_uidx
    ON org_unit_intake_source (org_unit_id, source_type, source_id);
