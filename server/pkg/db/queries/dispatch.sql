-- name: CreateDispatchOrigin :one
-- Records where a dispatch came from so the requester can be told when the
-- department delivers. The issue is always fresh, so a conflict means a
-- retry of the same create and the existing row wins.
INSERT INTO dispatch_origin (
    issue_id, workspace_id, requester_id, target_unit_id, target_project_id,
    channel_type, installation_id, chat_id, thread_id, origin_message_id,
    client_request_id, target_squad_id
)
VALUES ($1, $2, $3, $4, sqlc.narg('target_project_id'), $5, $6, $7, $8, $9, sqlc.narg('client_request_id'), sqlc.narg('target_squad_id'))
ON CONFLICT (issue_id) DO NOTHING
RETURNING *;

-- name: GetDispatchOrigin :one
SELECT * FROM dispatch_origin WHERE issue_id = $1;

-- name: DeleteDispatchOrigin :execrows
-- The requester removing one thread from their history. The issue and the
-- work in its workspace stay; only the origin row that lists it goes.
DELETE FROM dispatch_origin
WHERE issue_id = $1 AND workspace_id = $2 AND requester_id = $3;

-- name: GetDispatchByClient :one
-- Resolves a retried in-app send to the dispatch the first attempt created.
SELECT * FROM dispatch_origin
WHERE workspace_id = $1 AND requester_id = $2 AND client_request_id = $3;

-- name: ListDispatchesByRequester :many
-- Workbench history. leader_state summarizes the latest squad-leader run:
-- a failed run is only terminal once its retries are exhausted; queued,
-- claimed, running and deferred all read as "running" to the requester.
SELECT
    d.issue_id,
    d.target_unit_id,
    d.target_squad_id,
    d.target_project_id,
    p.name AS project_name,
    p.slug AS project_slug,
    d.created_at,
    d.notified_at,
    CASE
        WHEN lt.id IS NULL THEN 'none'
        WHEN lt.status = 'failed' AND lt.attempt >= lt.max_attempts THEN 'failed'
        WHEN lt.status IN ('queued', 'claimed', 'running', 'deferred') THEN 'running'
        ELSE lt.status
    END::text AS leader_state
FROM dispatch_origin d
LEFT JOIN workspace p ON p.id = d.target_project_id
LEFT JOIN LATERAL (
    SELECT id, status, attempt, max_attempts
    FROM agent_task_queue
    WHERE issue_id = d.issue_id AND is_leader_task
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) lt ON TRUE
WHERE d.workspace_id = $1 AND d.requester_id = $2
ORDER BY d.created_at DESC
LIMIT $3;

-- name: ListDispatchesForUnits :many
-- The department queue: dispatches aimed at one of the given units that are
-- still open, newest first. The stage is the issue's lifecycle category, read
-- from the workspace catalog so custom statuses land in the right bucket; the
-- built-in fallback keeps issues whose catalog row is missing visible instead
-- of dropping them out of the queue.
SELECT * FROM (
    SELECT
        d.issue_id,
        d.requester_id,
        d.target_unit_id,
        d.target_squad_id,
        d.created_at,
        d.notified_at,
        COALESCE(st.category, CASE i.status
            WHEN 'backlog' THEN 'unstarted'
            WHEN 'todo' THEN 'unstarted'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review' THEN 'started'
            WHEN 'blocked' THEN 'started'
            WHEN 'done' THEN 'done'
            WHEN 'cancelled' THEN 'closed'
            ELSE 'started'
        END)::text AS status_category,
        CASE
            WHEN lt.id IS NULL THEN 'none'
            WHEN lt.status = 'failed' AND lt.attempt >= lt.max_attempts THEN 'failed'
            WHEN lt.status IN ('queued', 'claimed', 'running', 'deferred') THEN 'running'
            ELSE lt.status
        END::text AS leader_state
    FROM dispatch_origin d
    JOIN issue i ON i.id = d.issue_id AND i.workspace_id = d.workspace_id
    LEFT JOIN issue_status st ON st.workspace_id = d.workspace_id AND st.key = i.status
    LEFT JOIN LATERAL (
        SELECT id, status, attempt, max_attempts
        FROM agent_task_queue
        WHERE issue_id = d.issue_id AND is_leader_task
        ORDER BY created_at DESC, id DESC
        LIMIT 1
    ) lt ON TRUE
    WHERE d.workspace_id = $1 AND d.target_unit_id = ANY($2::uuid[])
) queue
WHERE status_category IN ('unstarted', 'started')
ORDER BY created_at DESC
LIMIT $3;

-- name: ListOpenChatDispatches :many
-- Chat control surface: a requester's still-open dispatches across every
-- project they can see, in the order the conversation lists them. Personal
-- dispatches carry the private squad name instead of a department.
SELECT
    d.issue_id,
    d.workspace_id,
    d.target_unit_id,
    d.target_squad_id,
    d.chat_id,
    d.thread_id,
    d.created_at,
    i.number,
    i.title,
    i.status,
    w.slug,
    w.name AS workspace_name,
    w.issue_prefix,
    COALESCE(u.name, s.name, '')::text AS unit_name,
    (d.target_squad_id IS NOT NULL)::boolean AS personal,
    COALESCE(st.category, CASE i.status
        WHEN 'backlog' THEN 'unstarted'
        WHEN 'todo' THEN 'unstarted'
        WHEN 'in_progress' THEN 'started'
        WHEN 'in_review' THEN 'started'
        WHEN 'blocked' THEN 'started'
        WHEN 'done' THEN 'done'
        WHEN 'cancelled' THEN 'closed'
        ELSE 'started'
    END)::text AS status_category,
    CASE
        WHEN lt.id IS NULL THEN 'none'
        WHEN lt.status = 'failed' AND lt.attempt >= lt.max_attempts THEN 'failed'
        WHEN lt.status IN ('queued', 'claimed', 'running', 'deferred') THEN 'running'
        ELSE lt.status
    END::text AS leader_state
FROM dispatch_origin d
JOIN issue i ON i.id = d.issue_id
JOIN workspace w ON w.id = i.workspace_id
LEFT JOIN org_unit u ON u.id = d.target_unit_id
LEFT JOIN squad s ON s.id = d.target_squad_id AND s.workspace_id = d.workspace_id
LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status
LEFT JOIN LATERAL (
    SELECT id, status, attempt, max_attempts
    FROM agent_task_queue
    WHERE issue_id = d.issue_id AND is_leader_task
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) lt ON TRUE
WHERE d.requester_id = $1
  AND COALESCE(st.category, CASE i.status
        WHEN 'backlog' THEN 'unstarted'
        WHEN 'todo' THEN 'unstarted'
        WHEN 'in_progress' THEN 'started'
        WHEN 'in_review' THEN 'started'
        WHEN 'blocked' THEN 'started'
        WHEN 'done' THEN 'done'
        WHEN 'cancelled' THEN 'closed'
        ELSE 'started'
    END) IN ('unstarted', 'started')
ORDER BY d.created_at DESC
LIMIT $2;

-- name: GetOpenChatDispatchByThread :one
-- An in-thread message belongs to the most recent dispatch the requester
-- started in that same Lark topic.
SELECT
    d.issue_id,
    d.workspace_id,
    d.target_unit_id,
    d.target_squad_id,
    d.chat_id,
    d.thread_id,
    d.created_at,
    i.number,
    i.title,
    i.status,
    w.slug,
    w.name AS workspace_name,
    w.issue_prefix,
    COALESCE(u.name, s.name, '')::text AS unit_name,
    (d.target_squad_id IS NOT NULL)::boolean AS personal,
    COALESCE(st.category, 'started')::text AS status_category,
    'none'::text AS leader_state
FROM dispatch_origin d
JOIN issue i ON i.id = d.issue_id
JOIN workspace w ON w.id = i.workspace_id
LEFT JOIN org_unit u ON u.id = d.target_unit_id
LEFT JOIN squad s ON s.id = d.target_squad_id AND s.workspace_id = d.workspace_id
LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status
WHERE d.requester_id = $1
  AND d.chat_id = $2
  AND d.thread_id = $3
  AND COALESCE(st.category, 'started') IN ('unstarted', 'started')
ORDER BY d.created_at DESC
LIMIT 1;

-- name: ClaimDispatchStart :one
-- The requester hears once that the department began working. Retries and
-- replayed task events find the claim already taken.
UPDATE dispatch_origin
SET started_notified_at = now()
WHERE issue_id = $1 AND started_notified_at IS NULL AND channel_type = 'feishu'
RETURNING *;

-- name: ListOpenIntakeBlockersByIssue :many
-- Open blockers for a set of issues, so a queue can show why work is waiting
-- instead of leaving it parked with no explanation.
SELECT issue_id, done_text, missing_text, consequence_text
FROM intake_blocker
WHERE workspace_id = $1 AND issue_id = ANY(sqlc.arg(issue_ids)::uuid[]) AND state = 'open';

-- name: ClaimDispatchCompletion :one
-- Marks the completion notice as sent. The conditional update is the
-- idempotency guard: duplicate or replayed status events find no row.
UPDATE dispatch_origin
SET notified_at = now()
WHERE issue_id = $1 AND notified_at IS NULL
RETURNING *;

-- name: ClaimDispatchFailure :one
-- Marks the failure notice as sent, but only for the squad-leader task of this
-- dispatch: a child task failing is ordinary work, not the department giving
-- up. Like the completion claim, the conditional update is the idempotency
-- guard; retries that are still pending never reach here.
UPDATE dispatch_origin d
SET failure_notified_at = now()
WHERE d.issue_id = $1 AND d.failure_notified_at IS NULL
  AND EXISTS (
    SELECT 1 FROM agent_task_queue t
    WHERE t.id = $2 AND t.issue_id = d.issue_id AND t.is_leader_task
  )
RETURNING *;

-- name: UpdateIssueAssignee :one
-- Reassignment moves the root issue to the new department's squad. The
-- revision bump is what makes clients treat the pushed issue snapshot as
-- newer than the one they hold.
UPDATE issue
SET assignee_type = 'squad',
    assignee_id = $2,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: ReassignDispatchOrigin :one
-- A reassignment starts a fresh delivery: the previous completion and failure
-- notices no longer describe where the work is, so both claims are cleared.
UPDATE dispatch_origin
SET target_unit_id = $2,
    target_squad_id = NULL,
    target_project_id = NULL,
    notified_at = NULL,
    failure_notified_at = NULL,
    started_notified_at = NULL
WHERE issue_id = $1
RETURNING *;

-- name: SetIssueTreeOrgUnit :exec
-- A reassigned dispatch moves its whole issue tree to the new department, so
-- sub-issues the old squad already created follow the root's visibility.
WITH RECURSIVE tree AS (
    SELECT i.id FROM issue i
    WHERE i.id = @issue_id AND i.workspace_id = @workspace_id
    UNION
    SELECT c.id FROM issue c
    JOIN tree t ON c.parent_issue_id = t.id
)
UPDATE issue
SET org_unit_id = @org_unit_id
WHERE id IN (SELECT id FROM tree);
