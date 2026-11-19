-- The department directory: who belongs to which unit in this workspace. A
-- directory row is a candidate a project manager may pull into the project; it
-- grants no access. Reads join "user" for display fields.

-- name: ListDirectoryMembers :many
SELECT d.user_id, d.org_unit_id, u.name, u.email, u.avatar_url,
       EXISTS (
           SELECT 1 FROM member m
           WHERE m.workspace_id = d.workspace_id AND m.user_id = d.user_id
       ) AS in_project
FROM workspace_directory_member d
JOIN "user" u ON u.id = d.user_id
WHERE d.workspace_id = $1
ORDER BY u.name ASC;

-- name: ListDirectoryMembersInUnits :many
SELECT d.user_id, d.org_unit_id, u.name, u.email, u.avatar_url,
       EXISTS (
           SELECT 1 FROM member m
           WHERE m.workspace_id = d.workspace_id AND m.user_id = d.user_id
       ) AS in_project
FROM workspace_directory_member d
JOIN "user" u ON u.id = d.user_id
WHERE d.workspace_id = $1
  AND d.org_unit_id = ANY(sqlc.arg(unit_ids)::uuid[])
ORDER BY u.name ASC;

-- name: SearchDirectoryMembers :many
-- Candidate picker for the whole company directory: a project manager may
-- pull in colleagues from other departments, not just the owning subtree.
-- Server-side search keeps a company-sized directory off the wire.
SELECT d.user_id, d.org_unit_id, u.name, u.email, u.avatar_url,
       EXISTS (
           SELECT 1 FROM member m
           WHERE m.workspace_id = d.workspace_id AND m.user_id = d.user_id
       ) AS in_project
FROM workspace_directory_member d
JOIN "user" u ON u.id = d.user_id
WHERE d.workspace_id = $1
  AND (
      sqlc.arg(query)::text = ''
      OR u.name ILIKE '%' || sqlc.arg(query)::text || '%'
      OR u.email ILIKE '%' || sqlc.arg(query)::text || '%'
  )
ORDER BY u.name ASC
LIMIT 50;

-- name: GetDirectoryMember :one
SELECT d.user_id, d.org_unit_id, u.name, u.email, u.avatar_url
FROM workspace_directory_member d
JOIN "user" u ON u.id = d.user_id
WHERE d.workspace_id = $1 AND d.user_id = $2;

-- name: UpsertDirectoryMember :one
INSERT INTO workspace_directory_member (workspace_id, user_id, org_unit_id)
VALUES ($1, $2, sqlc.narg('org_unit_id'))
ON CONFLICT (workspace_id, user_id) DO UPDATE
SET org_unit_id = EXCLUDED.org_unit_id, updated_at = now()
RETURNING *;

-- name: InsertDirectoryMembers :exec
-- Bulk copy into a newly created project. The caller has already remapped unit
-- ids; a NULL unit stays NULL.
INSERT INTO workspace_directory_member (workspace_id, user_id, org_unit_id)
SELECT $1, members.user_id, units.org_unit_id
FROM unnest(sqlc.arg(user_ids)::uuid[]) WITH ORDINALITY AS members(user_id, ordinality)
JOIN unnest(sqlc.arg(org_unit_ids)::uuid[]) WITH ORDINALITY AS units(org_unit_id, ordinality)
  ON units.ordinality = members.ordinality
ON CONFLICT (workspace_id, user_id) DO NOTHING;

-- name: ListDirectoryMembersForCopy :many
-- A new project copies the whole directory of the workspace it takes its
-- department from, so the operator can later move it within the same company
-- tree without another sync.
SELECT d.user_id, d.org_unit_id
FROM workspace_directory_member d
WHERE d.workspace_id = $1
ORDER BY d.user_id ASC;
