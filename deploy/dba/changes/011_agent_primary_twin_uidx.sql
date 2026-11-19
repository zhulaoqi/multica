-- One primary twin per owner per workspace (migration 654). Archived agents
-- and hidden system carriers are excluded, so archiving a primary twin frees
-- the slot for whichever agent the owner promotes next.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_primary_twin_uidx
    ON agent (workspace_id, owner_id)
    WHERE is_primary AND archived_at IS NULL AND kind = 'user' AND owner_id IS NOT NULL;

INSERT INTO schema_migrations (version) VALUES ('011_agent_primary_twin_uidx');
