-- A projection is a project-side agent row that mirrors an organization agent
-- (source_agent_id). Identity, instructions, model settings and invocation
-- permission come from the source; secrets (custom_env, runtime_config,
-- mcp_config) stay behind so project admins cannot read them. The projection runs on the project
-- runtime that the same daemon registered for the same provider and profile.

-- name: GetAgentProjection :one
-- An archived projection still holds its name, so re-adding a source restores
-- that row instead of inserting a second one.
SELECT * FROM agent
WHERE workspace_id = @workspace_id
  AND source_agent_id = @source_agent_id
ORDER BY archived_at IS NULL DESC, created_at DESC
LIMIT 1;

-- name: CreateAgentProjection :one
INSERT INTO agent (
    workspace_id, source_agent_id, owner_id, name, description, avatar_url,
    runtime_mode, runtime_config, runtime_id, visibility, permission_mode,
    max_concurrent_tasks, instructions, custom_env, custom_args,
    conversation_starters, model, thinking_level, service_tier, intake_can,
    intake_cannot, org_unit_id, is_primary
)
SELECT
    @workspace_id, s.id, s.owner_id, s.name, s.description, s.avatar_url,
    s.runtime_mode, '{}'::jsonb,
    (
        SELECT pr.id
        FROM agent_runtime sr
        JOIN agent_runtime pr
          ON pr.workspace_id = @workspace_id
         AND pr.daemon_id = sr.daemon_id
         AND pr.provider = sr.provider
         AND pr.owner_id IS NOT DISTINCT FROM sr.owner_id
        LEFT JOIN runtime_profile sp ON sp.id = sr.profile_id
        LEFT JOIN runtime_profile pp ON pp.id = pr.profile_id
        WHERE sr.id = s.runtime_id
          AND (pr.profile_id IS NULL) = (sr.profile_id IS NULL)
          AND (sr.profile_id IS NULL OR pp.display_name = sp.display_name)
        ORDER BY pr.last_seen_at DESC NULLS LAST
        LIMIT 1
    ),
    s.visibility, s.permission_mode,
    s.max_concurrent_tasks, s.instructions, '{}'::jsonb, s.custom_args,
    s.conversation_starters, s.model, s.thinking_level, s.service_tier,
    s.intake_can, s.intake_cannot, s.org_unit_id,
    s.is_primary AND NOT EXISTS (
        SELECT 1 FROM agent o
        WHERE o.workspace_id = @workspace_id
          AND o.owner_id = s.owner_id
          AND o.kind = 'user'
          AND o.archived_at IS NULL
          AND o.is_primary
    )
FROM agent s
WHERE s.id = @source_agent_id
  AND s.kind = 'user'
  AND COALESCE(s.system_key, '') = ''
  AND s.archived_at IS NULL
RETURNING *;

-- name: SyncAgentProjection :one
-- Re-reads the source into one projection. is_primary follows the source but
-- never displaces another primary the owner already has in the project.
UPDATE agent AS p
SET name = s.name,
    description = s.description,
    avatar_url = s.avatar_url,
    runtime_mode = s.runtime_mode,
    runtime_id = (
        SELECT pr.id
        FROM agent_runtime sr
        JOIN agent_runtime pr
          ON pr.workspace_id = p.workspace_id
         AND pr.daemon_id = sr.daemon_id
         AND pr.provider = sr.provider
         AND pr.owner_id IS NOT DISTINCT FROM sr.owner_id
        LEFT JOIN runtime_profile sp ON sp.id = sr.profile_id
        LEFT JOIN runtime_profile pp ON pp.id = pr.profile_id
        WHERE sr.id = s.runtime_id
          AND (pr.profile_id IS NULL) = (sr.profile_id IS NULL)
          AND (sr.profile_id IS NULL OR pp.display_name = sp.display_name)
        ORDER BY pr.last_seen_at DESC NULLS LAST
        LIMIT 1
    ),
    visibility = s.visibility,
    permission_mode = s.permission_mode,
    max_concurrent_tasks = s.max_concurrent_tasks,
    instructions = s.instructions,
    custom_args = s.custom_args,
    conversation_starters = s.conversation_starters,
    model = s.model,
    thinking_level = s.thinking_level,
    service_tier = s.service_tier,
    intake_can = s.intake_can,
    intake_cannot = s.intake_cannot,
    org_unit_id = s.org_unit_id,
    owner_id = s.owner_id,
    is_primary = s.is_primary
        AND s.archived_at IS NULL
        AND p.archived_at IS NULL
        AND NOT EXISTS (
            SELECT 1 FROM agent o
            WHERE o.workspace_id = p.workspace_id
              AND o.owner_id = s.owner_id
              AND o.kind = 'user'
              AND o.archived_at IS NULL
              AND o.is_primary
              AND o.id <> p.id
        ),
    updated_at = now()
FROM agent s
WHERE p.id = @id
  AND s.id = p.source_agent_id
RETURNING p.*;

-- name: SyncAgentProjectionInvocationTargets :exec
-- Mirrors the source allow-list into one project projection. Workspace-wide
-- access is remapped to the projection's workspace; member and team ids are
-- organization-global and carry across unchanged.
WITH desired AS MATERIALIZED (
    SELECT
        p.id AS agent_id,
        t.target_type,
        CASE
            WHEN t.target_type = 'workspace' THEN p.workspace_id
            ELSE t.target_id
        END AS target_id,
        t.created_by
    FROM agent p
    JOIN agent_invocation_target t ON t.agent_id = p.source_agent_id
    WHERE p.id = @projection_id
),
upserted AS (
    INSERT INTO agent_invocation_target (
        agent_id, target_type, target_id, created_by
    )
    SELECT agent_id, target_type, target_id, created_by
    FROM desired
    ON CONFLICT (agent_id, target_type, target_id) DO UPDATE SET
        created_by = EXCLUDED.created_by,
        created_at = now()
    RETURNING 1
)
DELETE FROM agent_invocation_target current
WHERE current.agent_id = @projection_id
  AND (SELECT count(*) FROM upserted) >= 0
  AND NOT EXISTS (
      SELECT 1
      FROM desired
      WHERE desired.target_type = current.target_type
        AND desired.target_id IS NOT DISTINCT FROM current.target_id
  );

-- name: ClearStaleProjectionPrimaries :many
-- Runs before SyncAgentProjection when an owner's primary moves, so the new
-- primary's projection does not see the old one still holding the flag.
UPDATE agent AS p
SET is_primary = false, updated_at = now()
FROM agent s
WHERE p.owner_id = @owner_id
  AND p.source_agent_id = s.id
  AND p.is_primary
  AND NOT (s.is_primary AND s.archived_at IS NULL)
RETURNING p.*;

-- name: ListAgentProjectionsForSource :many
SELECT * FROM agent
WHERE source_agent_id = @source_agent_id
ORDER BY created_at ASC, id ASC;

-- name: ListAgentProjectionsForOwner :many
SELECT * FROM agent
WHERE owner_id = @owner_id
  AND source_agent_id IS NOT NULL
  AND archived_at IS NULL
ORDER BY created_at ASC, id ASC;

-- name: ListAgentProjectionsForDaemon :many
-- Active projections in one project whose source runs on the given daemon.
-- Re-synced after that daemon registers, which is when the project runtime
-- they map to appears.
SELECT p.* FROM agent p
JOIN agent s ON s.id = p.source_agent_id
JOIN agent_runtime sr ON sr.id = s.runtime_id
WHERE p.workspace_id = @workspace_id
  AND p.archived_at IS NULL
  AND sr.daemon_id = @daemon_id
ORDER BY p.created_at ASC, p.id ASC;

-- name: ArchiveAgentProjectionsForSource :many
-- Stamps the source's own archived_at so a later restore of the source brings
-- back exactly these rows and leaves projections a manager removed alone.
UPDATE agent
SET archived_at = @archived_at, archived_by = @archived_by, is_primary = false, updated_at = now()
WHERE source_agent_id = @source_agent_id
  AND archived_at IS NULL
RETURNING *;

-- name: RestoreAgentProjectionsForSource :many
UPDATE agent
SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE source_agent_id = @source_agent_id
  AND archived_at = @archived_at
RETURNING *;

-- name: ListMemberProjectWorkspaces :many
-- Department projects the user has joined. Each one gets a projection of the
-- user's organization twin.
SELECT m.workspace_id
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = @user_id
  AND NOT w.is_organization
  AND w.org_unit_id IS NOT NULL
ORDER BY m.created_at ASC;

-- name: ListProjectionCandidates :many
-- Organization agents a project manager may add: active user agents whose
-- owner has joined the project. projected is true when the project already
-- has an active projection of the agent.
SELECT
    s.*,
    EXISTS (
        SELECT 1 FROM agent p
        WHERE p.workspace_id = @project_id
          AND p.source_agent_id = s.id
          AND p.archived_at IS NULL
    )::boolean AS projected
FROM agent s
WHERE s.workspace_id = @organization_id
  AND s.kind = 'user'
  AND COALESCE(s.system_key, '') = ''
  AND s.archived_at IS NULL
  AND s.owner_id IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM member m
      WHERE m.workspace_id = @project_id
        AND m.user_id = s.owner_id
  )
ORDER BY s.name ASC, s.id ASC;
