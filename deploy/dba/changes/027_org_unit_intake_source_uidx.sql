-- One row per source per department (migration 670).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_unit_intake_source_uidx
    ON org_unit_intake_source (org_unit_id, source_type, source_id);

INSERT INTO schema_migrations (version) VALUES ('027_org_unit_intake_source_uidx');
