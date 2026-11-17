-- name: CreateDispatchOrigin :one
-- Records where a dispatch came from so the requester can be told when the
-- department delivers. The issue is always fresh, so a conflict means a
-- retry of the same create and the existing row wins.
INSERT INTO dispatch_origin (
    issue_id, workspace_id, requester_id, target_unit_id,
    channel_type, installation_id, chat_id, thread_id, client_request_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg('client_request_id'))
ON CONFLICT (issue_id) DO NOTHING
RETURNING *;

-- name: GetDispatchOrigin :one
SELECT * FROM dispatch_origin WHERE issue_id = $1;

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
    d.created_at,
    d.notified_at,
    CASE
        WHEN lt.id IS NULL THEN 'none'
        WHEN lt.status = 'failed' AND lt.attempt >= lt.max_attempts THEN 'failed'
        WHEN lt.status IN ('queued', 'claimed', 'running', 'deferred') THEN 'running'
        ELSE lt.status
    END::text AS leader_state
FROM dispatch_origin d
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

-- name: ClaimDispatchCompletion :one
-- Marks the completion notice as sent. The conditional update is the
-- idempotency guard: duplicate or replayed status events find no row.
UPDATE dispatch_origin
SET notified_at = now()
WHERE issue_id = $1 AND notified_at IS NULL
RETURNING *;
