-- name: ListOrgUnits :many
SELECT id, parent_id, name, lead_user_id, description, position
FROM org_unit
WHERE workspace_id = $1
ORDER BY position ASC, name ASC;

-- name: GetOrgUnitByParentAndName :one
SELECT id
FROM org_unit
WHERE workspace_id = $1
  AND parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)
  AND name = $2;

-- name: GetOrgUnit :one
SELECT id
FROM org_unit
WHERE id = $1 AND workspace_id = $2;

-- name: CreateOrgUnit :one
INSERT INTO org_unit (workspace_id, parent_id, name, lead_user_id, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: SetOrgUnitLead :exec
UPDATE org_unit
SET lead_user_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: SetAgentOrgUnit :exec
UPDATE agent
SET org_unit_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: FindAgentByName :one
SELECT id
FROM agent
WHERE workspace_id = $1 AND name = $2 AND archived_at IS NULL;

-- name: FindWorkspaceMemberByEmail :one
SELECT u.id
FROM "user" u
JOIN member m ON m.user_id = u.id AND m.workspace_id = $1
WHERE lower(u.email) = lower($2);

-- name: ListAgentsInOrgUnits :many
-- Includes agents with an empty unit so the org page can place them.
SELECT id, name, org_unit_id, intake_can, intake_cannot
FROM agent
WHERE workspace_id = $1
  AND archived_at IS NULL
ORDER BY name ASC;
-- name: RenameOrgUnit :exec
UPDATE org_unit
SET name = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: CountOrgUnitChildren :one
SELECT count(*)::int
FROM org_unit
WHERE workspace_id = $1 AND parent_id = $2;

-- name: ClearOrgUnitAgents :exec
UPDATE agent
SET org_unit_id = NULL, updated_at = now()
WHERE workspace_id = $1 AND org_unit_id = $2;

-- name: DeleteOrgUnit :exec
DELETE FROM org_unit
WHERE id = $1 AND workspace_id = $2;
