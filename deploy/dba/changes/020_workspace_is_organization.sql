-- Marks the single organization workspace that owns the company org chart
-- and every agent; other workspaces are projects that reference it (migration 663).
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS is_organization boolean NOT NULL DEFAULT false;

INSERT INTO schema_migrations (version) VALUES ('020_workspace_is_organization');
