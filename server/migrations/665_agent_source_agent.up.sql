-- A project agent with source_agent_id set is a read-only projection of an
-- organization agent; its identity and instructions sync from the source.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS source_agent_id uuid;
