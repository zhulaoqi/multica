-- A project agent with source_agent_id set is a read-only projection of an
-- organization agent; its identity and instructions sync from the source (migration 665).
ALTER TABLE agent ADD COLUMN IF NOT EXISTS source_agent_id uuid;

INSERT INTO schema_migrations (version) VALUES ('022_agent_source_agent');
