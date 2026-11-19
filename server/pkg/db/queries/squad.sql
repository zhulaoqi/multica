-- name: CreateSquad :one
INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, avatar_url, is_private)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSquad :one
SELECT * FROM squad WHERE id = $1;

-- name: GetSquadInWorkspace :one
SELECT * FROM squad WHERE id = $1 AND workspace_id = $2;

-- name: LockSquadForAutopilotAssignment :one
-- Stabilizes the squad-to-leader resolution while an active Autopilot is
-- created, retargeted, or resumed. FOR SHARE conflicts with an ordinary
-- leader_id update, so the caller subsequently locks the same leader Agent
-- whose row Runtime teardown serializes against.
SELECT * FROM squad
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: LockSquadForUpdate :one
-- Squad leader changes take the exclusive side of the same lock used by
-- Autopilot assignment. The handler then locks the proposed leader Agent and
-- pauses active squad Autopilots when that Agent is unbound.
SELECT * FROM squad
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListSquads :many
SELECT * FROM squad WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: GetPersonalSquadByCreator :one
-- The workbench's personal target reuses one live private squad per creator.
SELECT * FROM squad
WHERE workspace_id = $1 AND creator_id = $2 AND is_private AND archived_at IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: GetSquadByOrgUnit :one
-- The department's squad, if one has been generated. Org pages keep one
-- implicit squad per unit; additional squads stay manual and unbound.
SELECT * FROM squad
WHERE workspace_id = $1 AND org_unit_id = $2 AND archived_at IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: MarkSquadOrgManaged :exec
-- Adopts a squad into the org mirror. Only generated or explicitly synced
-- squads mirror their unit subtree; hand-attached squads stay manual.
UPDATE squad SET org_managed = true
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: ListSquadMemberPreviewRows :many
-- Static squad membership summary for list/hover previews. This deliberately
-- excludes derived runtime/task status; the squad detail members-status
-- endpoint owns live state.
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE s.workspace_id = $1 AND s.archived_at IS NULL
ORDER BY
    sm.squad_id ASC,
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListSquadMemberPreviewRowsBySquad :many
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE sm.squad_id = $1
ORDER BY
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListAllSquads :many
SELECT * FROM squad WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquad :one
UPDATE squad SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    wip_limit = COALESCE(sqlc.narg('wip_limit'), wip_limit),
    max_children = COALESCE(sqlc.narg('max_children'), max_children),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListOrgManagedSquads :many
-- Department squads whose leader and roster follow the organization tree.
SELECT * FROM squad
WHERE workspace_id = $1
  AND org_managed
  AND archived_at IS NULL
ORDER BY created_at ASC;

-- name: ArchiveSquad :one
UPDATE squad SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddSquadMember :one
INSERT INTO squad_member (squad_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpsertSquadMember :exec
-- Org sync re-runs on demand and after department moves; the same member must
-- settle to one row whose role and provenance follow the latest roster.
INSERT INTO squad_member (squad_id, member_type, member_id, role, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (squad_id, member_type, member_id)
DO UPDATE SET role = EXCLUDED.role, source = EXCLUDED.source;

-- name: RemoveSquadMember :execrows
DELETE FROM squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3;

-- name: RemoveSyncedSquadMember :execrows
-- Prunes a member the org sync added but the current department no longer
-- covers. Manually added members keep source='manual' and survive.
DELETE FROM squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3 AND source = $4;

-- name: ListSquadMembers :many
SELECT * FROM squad_member WHERE squad_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquadMemberRole :one
UPDATE squad_member SET role = $4
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsSquadMember :one
SELECT EXISTS(
    SELECT 1 FROM squad_member
    WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountSquadMembers :one
SELECT count(*) FROM squad_member WHERE squad_id = $1;

-- name: GetSquadByAssignee :one
-- Look up the squad when an issue is assigned to a squad.
SELECT s.* FROM squad s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListSquadsByMember :many
-- Find all squads a given entity belongs to in a workspace.
SELECT s.* FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
ORDER BY s.created_at ASC;

-- name: TransferSquadAssignees :exec
-- Transfer all issues assigned to a squad to the squad's leader agent.
UPDATE issue SET assignee_type = 'agent', assignee_id = $2, revision = revision + 1, updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: TransferSquadAutopilotsToLeader :exec
-- Mirrors TransferSquadAssignees for autopilot rows: when a squad is archived,
-- any autopilot still pointing at the squad would otherwise dangle and the
-- admission gate would skip every subsequent dispatch with "assignee squad
-- cannot be resolved". Rewrite the assignee in place to the leader agent so
-- the autopilot keeps firing under the same leader-only execution semantics
-- it had a moment before the archive (Path A from MUL-2429).
UPDATE autopilot
SET assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: ListSquadMemberStatusRows :many
-- Per-row join used to build the squad-members status view. One row per
-- (squad_member × in_flight_task); members with no in-flight task return a
-- single row with NULL task_* columns. Human members and agent members
-- with no agent row also return one row with NULL agent_/runtime_ columns.
-- waiting_local_directory stays in the row set so its issue remains visible,
-- but the handler only treats dispatched/running rows as working because the
-- squad status vocabulary has no queued bucket.
SELECT
    sm.id              AS squad_member_id,
    sm.member_type     AS member_type,
    sm.member_id       AS member_id,
    a.archived_at      AS agent_archived_at,
    ar.status          AS runtime_status,
    ar.last_seen_at    AS runtime_last_seen_at,
    atq.id             AS task_id,
    atq.status         AS task_status,
    atq.issue_id       AS task_issue_id,
    atq.dispatched_at  AS task_dispatched_at,
    i.number           AS issue_number,
    i.title            AS issue_title,
    i.status           AS issue_status
FROM squad_member sm
LEFT JOIN agent a
       ON sm.member_type = 'agent' AND a.id = sm.member_id
LEFT JOIN agent_runtime ar
       ON ar.id = a.runtime_id
LEFT JOIN agent_task_queue atq
       ON sm.member_type = 'agent'
      AND atq.agent_id = sm.member_id
      AND atq.status IN ('dispatched', 'running', 'waiting_local_directory')
LEFT JOIN issue i
       ON i.id = atq.issue_id
WHERE sm.squad_id = $1
ORDER BY sm.created_at ASC, atq.dispatched_at DESC NULLS LAST;
