-- One live personal squad per creator (migration 657).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_personal_uidx
    ON squad (workspace_id, creator_id)
    WHERE is_private AND archived_at IS NULL;

INSERT INTO schema_migrations (version) VALUES ('014_squad_personal_uidx');
