-- name: CreateDispatchOrigin :one
-- Records where a dispatch came from so the requester can be told when the
-- department delivers. The issue is always fresh, so a conflict means a
-- retry of the same create and the existing row wins.
INSERT INTO dispatch_origin (
    issue_id, workspace_id, requester_id, target_unit_id,
    channel_type, installation_id, chat_id, thread_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (issue_id) DO NOTHING
RETURNING *;

-- name: GetDispatchOrigin :one
SELECT * FROM dispatch_origin WHERE issue_id = $1;

-- name: ClaimDispatchCompletion :one
-- Marks the completion notice as sent. The conditional update is the
-- idempotency guard: duplicate or replayed status events find no row.
UPDATE dispatch_origin
SET notified_at = now()
WHERE issue_id = $1 AND notified_at IS NULL
RETURNING *;
