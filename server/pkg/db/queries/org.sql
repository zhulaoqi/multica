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
-- Includes agents with an empty unit so the org page can place them. Status is
-- the agent's own availability, which the org view surfaces as whether the
-- agent is waiting for collaborative scheduling. OwnerID lets the client match
-- an agent to its member (the "digital twin" binding); is_primary picks which
-- of several owned agents is the twin the org view shows on the member row.
SELECT id, name, owner_id, org_unit_id, intake_can, intake_cannot, status, runtime_mode, is_primary
FROM agent
WHERE workspace_id = $1
  AND archived_at IS NULL
ORDER BY name ASC;
-- name: ListRunnableLeaderAgents :many
-- Active agents whose runtime is online right now, the pool a department
-- squad can lead with after a lead change or a departure. Preference (the
-- lead's primary twin first) is resolved in Go so the query stays stable.
SELECT a.id, a.owner_id, a.org_unit_id, a.is_primary
FROM agent a
JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1
  AND a.archived_at IS NULL
  AND a.runtime_id IS NOT NULL
  AND r.status = 'online'
ORDER BY a.is_primary DESC, a.created_at ASC;

-- name: MovePrimaryTwinToOwnerUnit :execrows
-- The member's primary twin represents them in the organization, so it follows
-- them when their department placement changes.
UPDATE agent a
SET org_unit_id = m.org_unit_id, updated_at = now()
FROM member m
WHERE a.workspace_id = $1
  AND a.owner_id = $2
  AND a.is_primary
  AND m.workspace_id = a.workspace_id
  AND m.user_id = a.owner_id
  AND a.org_unit_id IS DISTINCT FROM m.org_unit_id;

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

-- name: ListSquadsInOrgUnits :many
-- Squads attached to the organisation tree, with the working count their
-- members currently report so the org view can show squads as waiting for or
-- already busy with collaborative scheduling.
SELECT
    s.id,
    s.name,
    s.org_unit_id,
    s.leader_id,
    s.wip_limit,
    s.max_children,
    count(DISTINCT sm.member_id) FILTER (WHERE sm.member_type = 'agent')::int AS agent_count,
    count(DISTINCT a.id) FILTER (
        WHERE sm.member_type = 'agent' AND a.status = 'working'
    )::int AS working_agents,
    (
        SELECT count(*)::int FROM issue i
        WHERE i.workspace_id = s.workspace_id
          AND i.assignee_type = 'squad' AND i.assignee_id = s.id
          AND i.status IN ('todo', 'in_progress')
    ) AS active_issues,
    (
        SELECT count(*)::int FROM issue i
        WHERE i.workspace_id = s.workspace_id
          AND i.assignee_type = 'squad' AND i.assignee_id = s.id
          AND i.status = 'in_review'
    ) AS review_issues,
    (
        SELECT count(*)::int FROM issue i
        WHERE i.workspace_id = s.workspace_id
          AND i.assignee_type = 'squad' AND i.assignee_id = s.id
          AND i.status = 'blocked'
    ) AS blocked_issues
FROM squad s
LEFT JOIN squad_member sm ON sm.squad_id = s.id
LEFT JOIN agent a ON a.id = sm.member_id
WHERE s.workspace_id = $1
  AND s.archived_at IS NULL
  -- Personal squads belong to one member and are not part of the department
  -- structure, so the org tree never lists them.
  AND NOT s.is_private
GROUP BY s.id
ORDER BY s.name ASC;

-- name: SetSquadOrgUnit :exec
UPDATE squad
SET org_unit_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: ClearOrgUnitSquads :exec
UPDATE squad
SET org_unit_id = NULL, updated_at = now()
WHERE workspace_id = $1 AND org_unit_id = $2;

-- name: DeleteOrgUnit :exec
DELETE FROM org_unit
WHERE id = $1 AND workspace_id = $2;

-- name: ListOrgUnitMembers :many
-- Every workspace member with their org unit (NULL when unassigned). The org
-- page needs the full roster to pick unit leads and to list unassigned people.
SELECT m.user_id, m.org_unit_id, u.name, u.email, u.avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY u.name;

-- name: ListOrgUnitMemberIDs :many
-- Every project member's unit assignment, ids only. Manager surfaces (team
-- runtime visibility) walk the unit tree in Go and do not want the full
-- roster's names/emails — the org page's ListOrgUnitMembers is far too heavy
-- for a list endpoint that runs on every Runtimes page load. Every member row
-- is an explicit collaborator, including people from other departments.
SELECT m.user_id, m.org_unit_id
FROM member m
WHERE m.workspace_id = $1
ORDER BY m.user_id;

-- name: SetMemberOrgUnit :exec
UPDATE member
SET org_unit_id = $3
WHERE workspace_id = $1 AND user_id = $2;

-- name: CountOrgUnitMembers :one
SELECT count(*)::int
FROM member
WHERE workspace_id = $1 AND org_unit_id = $2;

-- name: CountDirectoryMembersInUnit :one
-- A unit that still describes directory members cannot be deleted: they would
-- lose their placement, not their project membership.
SELECT count(*)::int
FROM workspace_directory_member
WHERE workspace_id = $1 AND org_unit_id = $2;

-- name: CreateOrgUnitClone :one
-- Copy one unit into a newly created workspace when it takes over a department
-- from an existing one. The caller remaps parent_id and lead_user_id.
INSERT INTO org_unit (workspace_id, parent_id, name, lead_user_id, description, position, external_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: ListSyncedOrgUnits :many
-- Units this workspace imported from a directory, keyed by the directory's id.
-- Manual units have no external id and are left alone by sync.
SELECT id, parent_id, name, external_id, lead_user_id
FROM org_unit
WHERE workspace_id = $1 AND external_id IS NOT NULL;

-- name: CreateOrgUnitWithExternal :one
INSERT INTO org_unit (workspace_id, parent_id, name, external_id)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: UpdateOrgUnitFromExternal :exec
-- The caller keeps the owning department's root parent unchanged (see
-- upsertDirectoryUnits): a project reads its scope from that unit.
UPDATE org_unit
SET name = $3, parent_id = $4, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: SetOrgUnitLeadIfEmpty :execrows
-- Sync only fills a lead the workspace has not chosen yet; manual assignments win.
UPDATE org_unit
SET lead_user_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND lead_user_id IS NULL;

-- name: InsertOrgSyncRun :one
INSERT INTO org_sync_run (workspace_id, summary)
VALUES ($1, sqlc.arg(summary)::jsonb)
RETURNING id, created_at;

-- name: GetLatestOrgSyncRun :one
SELECT summary, created_at
FROM org_sync_run
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: ListOrgUnitsForCopy :many
-- A new project copies the complete tree of the workspace it takes its
-- department from, preserving manual units (external_id NULL) as well as
-- synced ones.
SELECT id, parent_id, name, lead_user_id, description, position, external_id
FROM org_unit
WHERE workspace_id = $1
ORDER BY position ASC, name ASC;

-- name: ListWorkspaceFeishuIdentities :many
-- Directory-linked people in one legacy workspace, so a sync can report who
-- disappeared upstream without touching manually invited members.
SELECT i.user_id, i.email, i.name
FROM feishu_identity i
JOIN member m ON m.user_id = i.user_id AND m.workspace_id = $1;

-- name: ListWorkspaceDirectoryFeishuIdentities :many
-- The same report for a department project: its directory rows carry the
-- company roster even though most people are not project members.
SELECT i.user_id, i.email, i.name
FROM feishu_identity i
JOIN workspace_directory_member d ON d.user_id = i.user_id AND d.workspace_id = $1;
