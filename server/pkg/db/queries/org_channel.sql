-- Organization-wide IM installation (migration 658). The organization bot is
-- deployment-scoped: one row per channel type, not per workspace/agent. The
-- app secret stays in deployment env, so config only carries the routing
-- identity (app_id, region) for operators.

-- name: EnsureOrgChannelInstallation :one
-- Boot-time upsert. Re-running with the same app id is a no-op; changing the
-- configured app rewrites the row so the supervisor's fingerprint rotates.
INSERT INTO org_channel_installation (channel_type, config, status)
VALUES (sqlc.arg('channel_type'), sqlc.arg('config'), 'active')
ON CONFLICT (channel_type) DO UPDATE SET
    config     = EXCLUDED.config,
    status     = 'active',
    updated_at = now()
RETURNING *;

-- name: GetOrgChannelInstallationByType :one
SELECT * FROM org_channel_installation
WHERE channel_type = sqlc.arg('channel_type');

-- name: GetOrgChannelInstallationByID :one
-- Dispatch completion resolves an origin installation id to decide whether the
-- reply goes through a per-agent bot or the organization app.
SELECT * FROM org_channel_installation
WHERE id = sqlc.arg('id');

-- name: AcquireOrgChannelWSLease :one
-- Same CAS shape as AcquireChannelWSLease: grant when unleased, expired, or
-- already ours (renewal).
UPDATE org_channel_installation
SET ws_lease_token      = sqlc.arg('new_token'),
    ws_lease_expires_at = sqlc.arg('new_expires_at'),
    updated_at          = now()
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseOrgChannelWSLease :exec
UPDATE org_channel_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = sqlc.arg('id')
  AND ws_lease_token = sqlc.arg('current_token');

-- name: ListDispatchProjectsForUser :many
-- Projects a chat sender can dispatch into: the ListWorkspaces access rule
-- plus a live primary agent (twin), because a dispatch is answered by the
-- requester's own Agent. Workspaces without an org tree are skipped: /dispatch
-- targets a department.
WITH RECURSIVE ancestors AS (
    SELECT w.id AS workspace_id, u.id AS unit_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id AND u.workspace_id = w.id
    WHERE w.org_unit_id IS NOT NULL
    UNION ALL
    SELECT a.workspace_id, parent.id
    FROM ancestors a
    JOIN org_unit child ON child.id = a.unit_id AND child.workspace_id = a.workspace_id
    JOIN org_unit parent ON parent.id = child.parent_id AND parent.workspace_id = child.workspace_id
)
SELECT w.id, w.name, w.slug, w.issue_prefix,
       COALESCE(root.name, '') AS department_name
FROM workspace w
LEFT JOIN org_unit root ON root.id = w.org_unit_id AND root.workspace_id = w.id
WHERE (
        EXISTS (
            SELECT 1 FROM member m
            WHERE m.workspace_id = w.id AND m.user_id = sqlc.arg('user_id')
        )
        OR EXISTS (
            SELECT 1
            FROM ancestors a
            JOIN org_unit unit ON unit.id = a.unit_id AND unit.workspace_id = a.workspace_id
            WHERE a.workspace_id = w.id AND unit.lead_user_id = sqlc.arg('user_id')
        )
    )
  AND EXISTS (
        SELECT 1 FROM agent a
        WHERE a.workspace_id = w.id
          AND a.owner_id = sqlc.arg('user_id')
          AND a.is_primary
          AND a.archived_at IS NULL
    )
  AND EXISTS (SELECT 1 FROM org_unit u WHERE u.workspace_id = w.id)
ORDER BY w.name ASC, w.id ASC;
