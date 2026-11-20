-- Department an organization-workspace issue belongs to; drives
-- department-scoped visibility. NULL in project workspaces (migration 667).
ALTER TABLE issue ADD COLUMN IF NOT EXISTS org_unit_id uuid;

INSERT INTO schema_migrations (version) VALUES ('024_issue_org_unit');
