-- Remembered workbench target for the organization Feishu bot. One row per
-- person per chat. The bot rewrites the whole row; it does not patch columns.

-- name: GetOrgBotChatContext :one
SELECT * FROM org_bot_chat_context
WHERE installation_id = sqlc.arg('installation_id')
  AND chat_id = sqlc.arg('chat_id')
  AND user_id = sqlc.arg('user_id');

-- name: UpsertOrgBotChatContext :exec
INSERT INTO org_bot_chat_context (
    installation_id, chat_id, user_id,
    target_kind, unit_id, unit_path, project_id, project_name,
    awaiting, pending_text, pending_candidates,
    last_issue_id, last_issue_identifier, updated_at
) VALUES (
    sqlc.arg('installation_id'), sqlc.arg('chat_id'), sqlc.arg('user_id'),
    sqlc.arg('target_kind'), sqlc.arg('unit_id'), sqlc.arg('unit_path'),
    sqlc.arg('project_id'), sqlc.arg('project_name'),
    sqlc.arg('awaiting'), sqlc.arg('pending_text'), sqlc.arg('pending_candidates'),
    sqlc.arg('last_issue_id'), sqlc.arg('last_issue_identifier'), now()
)
ON CONFLICT (installation_id, chat_id, user_id) DO UPDATE SET
    target_kind = EXCLUDED.target_kind,
    unit_id = EXCLUDED.unit_id,
    unit_path = EXCLUDED.unit_path,
    project_id = EXCLUDED.project_id,
    project_name = EXCLUDED.project_name,
    awaiting = EXCLUDED.awaiting,
    pending_text = EXCLUDED.pending_text,
    pending_candidates = EXCLUDED.pending_candidates,
    last_issue_id = EXCLUDED.last_issue_id,
    last_issue_identifier = EXCLUDED.last_issue_identifier,
    updated_at = now();
