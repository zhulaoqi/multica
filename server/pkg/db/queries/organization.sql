-- The organization workspace owns the only department tree and the company
-- roster: one member row per employee, created by directory sync. Projects
-- point their owning department (workspace.org_unit_id) and their members'
-- placements (member.org_unit_id) at units in that tree.

-- name: GetOrganizationWorkspace :one
SELECT * FROM workspace
WHERE is_organization
LIMIT 1;

-- name: SearchOrganizationMembers :many
-- Candidate picker for a project: anyone in the company may be pulled in, not
-- just the owning subtree. Server-side search keeps the roster off the wire.
SELECT m.user_id, m.org_unit_id, u.name, u.email, u.avatar_url,
       EXISTS (
           SELECT 1 FROM member p
           WHERE p.workspace_id = sqlc.arg(project_id) AND p.user_id = m.user_id
       ) AS in_project
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg(organization_id)
  AND (
      sqlc.arg(query)::text = ''
      OR u.name ILIKE '%' || sqlc.arg(query)::text || '%'
      OR u.email ILIKE '%' || sqlc.arg(query)::text || '%'
  )
ORDER BY u.name ASC
LIMIT 50;

-- name: GetOrganizationMember :one
SELECT m.user_id, m.org_unit_id, u.name, u.email, u.avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg(organization_id) AND m.user_id = sqlc.arg(user_id);

-- name: PropagateOrganizationPlacements :execrows
-- A person's department is decided once, in the organization workspace; every
-- project row of theirs follows it. Unplaced organization rows leave project
-- placements alone.
UPDATE member p
SET org_unit_id = o.org_unit_id
FROM member o
WHERE o.workspace_id = sqlc.arg(organization_id)
  AND p.user_id = o.user_id
  AND p.workspace_id <> o.workspace_id
  AND o.org_unit_id IS NOT NULL
  AND p.org_unit_id IS DISTINCT FROM o.org_unit_id;

-- name: AlignProjectTwinUnits :execrows
-- Primary twins outside the organization workspace follow their owner's
-- project placement after PropagateOrganizationPlacements. Projections take
-- their unit from their source instead (AlignProjectionUnits).
UPDATE agent a
SET org_unit_id = m.org_unit_id, updated_at = now()
FROM member m
WHERE a.workspace_id <> sqlc.arg(organization_id)
  AND a.is_primary
  AND a.source_agent_id IS NULL
  AND m.workspace_id = a.workspace_id
  AND m.user_id = a.owner_id
  AND m.org_unit_id IS NOT NULL
  AND a.org_unit_id IS DISTINCT FROM m.org_unit_id;

-- name: AlignProjectionUnits :execrows
-- Projections carry their organization source's department.
UPDATE agent p
SET org_unit_id = s.org_unit_id, updated_at = now()
FROM agent s
WHERE p.source_agent_id = s.id
  AND p.org_unit_id IS DISTINCT FROM s.org_unit_id;

-- name: ListProjectsMissingLeadOwner :many
-- Projects whose owning department's lead is not yet a stored owner. The lead
-- always owns the project; sync and lead changes heal the gap.
SELECT w.id AS workspace_id, w.org_unit_id AS root_unit_id, u.lead_user_id
FROM workspace w
JOIN org_unit u ON u.id = w.org_unit_id
WHERE u.lead_user_id IS NOT NULL
  AND (sqlc.narg('unit_id')::uuid IS NULL OR w.org_unit_id = sqlc.narg('unit_id')::uuid)
  AND NOT EXISTS (
      SELECT 1 FROM member m
      WHERE m.workspace_id = w.id AND m.user_id = u.lead_user_id AND m.role = 'owner'
  )
ORDER BY w.created_at ASC;

-- name: ListOrganizationMembersInUnits :many
-- The organization roster inside a project's department subtree, for the
-- project org page. in_project marks who already joined the project.
SELECT m.user_id, m.org_unit_id, u.name, u.email, u.avatar_url,
       EXISTS (
           SELECT 1 FROM member p
           WHERE p.workspace_id = sqlc.arg(project_id) AND p.user_id = m.user_id
       ) AS in_project
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg(organization_id)
  AND m.org_unit_id = ANY(sqlc.arg(unit_ids)::uuid[])
ORDER BY u.name ASC;

-- name: CountWorkspacesOwnedByUnit :one
SELECT count(*)::int FROM workspace WHERE org_unit_id = $1;
