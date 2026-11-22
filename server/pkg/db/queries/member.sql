-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListWorkspaceManagerUserIDs :many
-- Project managers for notifications: stored owners/admins plus the lead of
-- the owning department, whose stored role may still be member.
SELECT m.user_id FROM member m
JOIN workspace w ON w.id = m.workspace_id
LEFT JOIN org_unit root ON root.id = w.org_unit_id
WHERE m.workspace_id = $1
  AND (m.role IN ('owner', 'admin') OR root.lead_user_id = m.user_id)
ORDER BY m.created_at ASC;

-- name: ListUserWorkspaceMemberships :many
-- Every workspace the person holds an explicit member row in, wherever they
-- are placed. The project intake gate treats such a row as permission.
SELECT workspace_id FROM member WHERE user_id = $1;

-- name: GetMember :one
SELECT * FROM member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2;

-- name: GetRealtimeMemberships :many
-- Realtime fanout follows content access: every explicit member row, plus
-- department leads above the owning department, who inherit access upward.
WITH RECURSIVE ancestors AS (
    SELECT w.id AS workspace_id, u.id AS unit_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id
    WHERE w.org_unit_id IS NOT NULL
      AND w.id = ANY(sqlc.arg(workspace_ids)::uuid[])
    UNION ALL
    SELECT a.workspace_id, parent.id
    FROM ancestors a
    JOIN org_unit child ON child.id = a.unit_id
    JOIN org_unit parent ON parent.id = child.parent_id
)
SELECT m.user_id, m.workspace_id
FROM member m
JOIN (
    SELECT unnest(sqlc.arg(user_ids)::uuid[]) AS user_id,
           unnest(sqlc.arg(workspace_ids)::uuid[]) AS workspace_id
) AS requested
    ON m.user_id = requested.user_id AND m.workspace_id = requested.workspace_id
UNION
SELECT requested.user_id, requested.workspace_id
FROM (
    SELECT unnest(sqlc.arg(user_ids)::uuid[]) AS user_id,
           unnest(sqlc.arg(workspace_ids)::uuid[]) AS workspace_id
) AS requested
JOIN ancestors a ON a.workspace_id = requested.workspace_id
JOIN org_unit unit ON unit.id = a.unit_id
WHERE unit.lead_user_id = requested.user_id;

-- name: GetWorkspaceAccess :one
-- One row with everything the access policy needs: the owning department,
-- whether it still exists, the caller's member row, whether it sits in the
-- department subtree, and whether the caller leads the owning department or an
-- ancestor. Scalar subqueries avoid LEFT JOIN nullability on every column.
WITH RECURSIVE scope AS (
    SELECT w.id AS workspace_id, u.id AS unit_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id
    WHERE w.id = sqlc.arg(workspace_id)
    UNION ALL
    SELECT scope.workspace_id, child.id
    FROM scope
    JOIN org_unit child ON child.parent_id = scope.unit_id
),
ancestors AS (
    SELECT w.id AS workspace_id, u.id AS unit_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id
    WHERE w.id = sqlc.arg(workspace_id)
    UNION ALL
    SELECT a.workspace_id, parent.id
    FROM ancestors a
    JOIN org_unit child ON child.id = a.unit_id
    JOIN org_unit parent ON parent.id = child.parent_id
)
SELECT
    w.id AS workspace_id,
    w.is_organization,
    w.org_unit_id AS root_unit_id,
    (root.id IS NOT NULL)::bool AS root_exists,
    COALESCE(root.lead_user_id, '00000000-0000-0000-0000-000000000000'::uuid) AS root_lead_user_id,
    m.id AS member_id,
    COALESCE(m.role, '') AS role,
    m.org_unit_id AS member_org_unit_id,
    m.created_at AS member_created_at,
    (
        m.org_unit_id IS NOT NULL
        AND m.org_unit_id IN (SELECT unit_id FROM scope)
    )::bool AS in_subtree,
    EXISTS (
        SELECT 1
        FROM ancestors a
        JOIN org_unit unit ON unit.id = a.unit_id
        WHERE a.workspace_id = w.id AND unit.lead_user_id = sqlc.arg(user_id)
    )::bool AS ancestor_lead
FROM workspace w
LEFT JOIN org_unit root ON root.id = w.org_unit_id
LEFT JOIN member m ON m.workspace_id = w.id AND m.user_id = sqlc.arg(user_id)
WHERE w.id = sqlc.arg(workspace_id);

-- name: ListWorkspaceSubtreeUnits :many
-- The project's department subtree. Empty for a legacy workspace, which still
-- reads the whole tree.
WITH RECURSIVE scope AS (
    SELECT u.id, u.parent_id, u.name, u.lead_user_id, u.description, u.position
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id
    WHERE w.id = $1
    UNION ALL
    SELECT child.id, child.parent_id, child.name, child.lead_user_id,
           child.description, child.position
    FROM scope
    JOIN org_unit child ON child.parent_id = scope.id
)
SELECT id, parent_id, name, lead_user_id, description, position
FROM scope
ORDER BY position ASC, name ASC;

-- name: ListProjectMemberUserIDs :many
-- Everyone who may act in the project: every explicit member row (wherever
-- that person is placed) plus department leads above the owning department,
-- who inherit access upward.
WITH RECURSIVE ancestors AS (
    SELECT u.id AS unit_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id
    WHERE w.id = $1 AND w.org_unit_id IS NOT NULL
    UNION ALL
    SELECT parent.id
    FROM ancestors a
    JOIN org_unit child ON child.id = a.unit_id
    JOIN org_unit parent ON parent.id = child.parent_id
)
SELECT DISTINCT user_id FROM (
    SELECT m.user_id
    FROM member m
    WHERE m.workspace_id = $1
    UNION ALL
    SELECT unit.lead_user_id
    FROM ancestors a
    JOIN org_unit unit ON unit.id = a.unit_id
    WHERE unit.lead_user_id IS NOT NULL
) members;

-- name: CreateMember :one
INSERT INTO member (workspace_id, user_id, role, org_unit_id)
VALUES ($1, $2, $3, sqlc.narg('org_unit_id'))
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM member WHERE id = $1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at, m.org_unit_id,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url,
       (root.lead_user_id IS NOT NULL AND root.lead_user_id = m.user_id) as is_org_lead
FROM member m
JOIN "user" u ON u.id = m.user_id
JOIN workspace w ON w.id = m.workspace_id
LEFT JOIN org_unit root ON root.id = w.org_unit_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;

-- name: ListOwnedWorkspaceMemberRows :many
-- One-time project-member cleanup: every member row of a department-owned
-- workspace together with the owning department and its lead. The LEFT JOINs
-- keep a workspace whose lead has no member row, which the cleanup promotes
-- before computing the removal set. Archived and directory rows are not part
-- of the decision; only member rows are.
SELECT
    w.id AS workspace_id,
    w.name AS workspace_name,
    w.org_unit_id AS root_unit_id,
    root.lead_user_id AS root_lead_user_id,
    lead_user.name AS root_lead_name,
    lead_user.email AS root_lead_email,
    m.id AS member_id,
    m.user_id,
    m.role,
    m.org_unit_id AS member_org_unit_id,
    member_unit.name AS member_unit_name,
    member_user.name AS member_name,
    member_user.email AS member_email
FROM workspace w
JOIN org_unit root ON root.id = w.org_unit_id
LEFT JOIN "user" lead_user ON lead_user.id = root.lead_user_id
LEFT JOIN member m ON m.workspace_id = w.id
LEFT JOIN "user" member_user ON member_user.id = m.user_id
LEFT JOIN org_unit member_unit ON member_unit.id = m.org_unit_id
WHERE w.org_unit_id IS NOT NULL
ORDER BY w.created_at ASC, w.id ASC, m.created_at ASC, m.id ASC;
