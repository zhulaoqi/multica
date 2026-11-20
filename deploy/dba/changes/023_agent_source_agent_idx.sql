-- Finds every projection of an organization agent when the source changes (migration 666).
CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_source_agent_idx
    ON agent (source_agent_id)
    WHERE source_agent_id IS NOT NULL;

INSERT INTO schema_migrations (version) VALUES ('023_agent_source_agent_idx');
