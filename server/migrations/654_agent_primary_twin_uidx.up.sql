CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_primary_twin_uidx
    ON agent (workspace_id, owner_id)
    WHERE is_primary AND archived_at IS NULL AND kind = 'user' AND owner_id IS NOT NULL;
