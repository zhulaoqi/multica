-- One live personal squad per creator; the workbench entry dispatches to it.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_personal_uidx
    ON squad (workspace_id, creator_id)
    WHERE is_private AND archived_at IS NULL;
