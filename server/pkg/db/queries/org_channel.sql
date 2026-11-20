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
