-- Who belongs to which department, per workspace, independent of project
-- membership (migration 651). A directory row is a candidate the department
-- lead may pull into the project; it grants no access by itself.
CREATE TABLE IF NOT EXISTS workspace_directory_member (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL,
  user_id uuid NOT NULL,
  org_unit_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO workspace_directory_member (workspace_id, user_id, org_unit_id, created_at, updated_at)
SELECT m.workspace_id, m.user_id, m.org_unit_id, m.created_at, now()
FROM member m
WHERE NOT EXISTS (
  SELECT 1 FROM workspace_directory_member d
  WHERE d.workspace_id = m.workspace_id AND d.user_id = m.user_id
);

INSERT INTO schema_migrations (version) VALUES ('008_workspace_directory_member');
