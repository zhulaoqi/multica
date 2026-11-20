-- Department an organization-workspace issue belongs to; drives
-- department-scoped visibility. NULL in project workspaces.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS org_unit_id uuid;
