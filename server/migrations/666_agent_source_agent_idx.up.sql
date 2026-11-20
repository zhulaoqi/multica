-- Finds every projection of an organization agent when the source changes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_source_agent_idx
    ON agent (source_agent_id)
    WHERE source_agent_id IS NOT NULL;
