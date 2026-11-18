-- Who belongs to which department, per workspace, independent of project
-- membership. A directory row is a candidate the department lead may pull into
-- the project; it grants no access by itself. The workspace_directory_member_uidx
-- migration adds the unique index afterwards so this backfill can run without a
-- unique constraint in place.
CREATE TABLE IF NOT EXISTS workspace_directory_member (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    org_unit_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Existing member rows are the only directory source before the first
-- department-scoped sync. Keep their placement so a removed member remains
-- selectable.
INSERT INTO workspace_directory_member (workspace_id, user_id, org_unit_id, created_at, updated_at)
SELECT m.workspace_id, m.user_id, m.org_unit_id, m.created_at, now()
FROM member m
WHERE NOT EXISTS (
    SELECT 1 FROM workspace_directory_member d
    WHERE d.workspace_id = m.workspace_id AND d.user_id = m.user_id
);
