-- One-time conversion used by server/cmd/consolidate_org: projects stop owning
-- a department tree and their agents move into the organization workspace.

-- name: MarkWorkspaceOrganization :one
UPDATE workspace SET is_organization = true, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAllOrgUnits :many
-- in_project marks units of a department project's own tree, the ones being
-- retired. Flat workspaces keep their trees.
SELECT u.id, u.workspace_id, u.parent_id, u.name, u.external_id, u.lead_user_id,
       (NOT w.is_organization AND w.org_unit_id IS NOT NULL)::bool AS in_project
FROM org_unit u
JOIN workspace w ON w.id = u.workspace_id
ORDER BY u.workspace_id, u.created_at ASC, u.id ASC;

-- name: ListReferencedOrgUnitIDs :many
-- Every unit id a row still points at. A unit outside the organization that
-- appears here must be remapped before the project trees are deleted.
SELECT DISTINCT ref.unit_id::uuid AS unit_id
FROM (
    SELECT org_unit_id AS unit_id FROM workspace WHERE org_unit_id IS NOT NULL
    UNION ALL SELECT org_unit_id FROM member WHERE org_unit_id IS NOT NULL
    UNION ALL SELECT org_unit_id FROM agent WHERE org_unit_id IS NOT NULL
    UNION ALL SELECT org_unit_id FROM squad WHERE org_unit_id IS NOT NULL
    UNION ALL SELECT org_unit_id FROM issue WHERE org_unit_id IS NOT NULL
    UNION ALL SELECT target_unit_id FROM dispatch_origin WHERE target_unit_id IS NOT NULL
) ref;

-- name: RemapWorkspaceUnits :execrows
UPDATE workspace t SET org_unit_id = @new_id::uuid, updated_at = now()
WHERE t.org_unit_id = @old_id;

-- name: RemapMemberUnits :execrows
UPDATE member t SET org_unit_id = @new_id::uuid
WHERE t.org_unit_id = @old_id;

-- name: RemapAgentUnits :execrows
UPDATE agent t SET org_unit_id = @new_id::uuid, updated_at = now()
WHERE t.org_unit_id = @old_id;

-- name: RemapSquadUnits :execrows
UPDATE squad t SET org_unit_id = @new_id::uuid
WHERE t.org_unit_id = @old_id;

-- name: RemapIssueUnits :execrows
UPDATE issue t SET org_unit_id = @new_id::uuid
WHERE t.org_unit_id = @old_id;

-- name: RemapDispatchOriginUnits :execrows
UPDATE dispatch_origin t SET target_unit_id = @new_id::uuid
WHERE t.target_unit_id = @old_id;

-- name: CountDirectoryMembers :one
SELECT count(*)::int FROM workspace_directory_member;

-- name: DeleteAllDirectoryMembers :execrows
-- workspace_directory_member is retired: the organization roster replaced it.
DELETE FROM workspace_directory_member;

-- name: DeleteProjectOrgUnits :execrows
DELETE FROM org_unit u
USING workspace w
WHERE w.id = u.workspace_id
  AND NOT w.is_organization
  AND w.org_unit_id IS NOT NULL;

-- name: ListProjectAgentsToConsolidate :many
-- Owned user agents that department projects still keep for themselves.
-- Primaries come first and the most recently updated wins, so it becomes the
-- organization primary when one owner has twins in several projects.
SELECT a.*, w.slug AS workspace_slug
FROM agent a
JOIN workspace w ON w.id = a.workspace_id
WHERE NOT w.is_organization
  AND w.org_unit_id IS NOT NULL
  AND a.kind = 'user'
  AND a.archived_at IS NULL
  AND a.source_agent_id IS NULL
  AND a.owner_id IS NOT NULL
  AND COALESCE(a.system_key, '') = ''
ORDER BY a.is_primary DESC, a.updated_at DESC, a.id ASC;

-- name: CountAgentSkillsForAgents :one
SELECT count(*)::int FROM agent_skill WHERE agent_id = ANY(@agent_ids::uuid[]);

-- name: EnsureOrganizationRuntimeProfile :one
-- The organization's copy of a project's custom runtime profile, keyed by
-- display name like the projection runtime mapping.
INSERT INTO runtime_profile (
    workspace_id, display_name, protocol_family, command_name, description,
    fixed_args, visibility, created_by, enabled, runtime_type
)
SELECT @organization_id, p.display_name, p.protocol_family, p.command_name, p.description,
       p.fixed_args, p.visibility, p.created_by, p.enabled, p.runtime_type
FROM runtime_profile p
WHERE p.id = @source_profile_id
ON CONFLICT (workspace_id, display_name) DO UPDATE SET display_name = EXCLUDED.display_name
RETURNING *;

-- name: EnsureOrganizationRuntime :one
-- Pre-creates, offline, the runtime the same daemon will register in the
-- organization. Registration upserts on the same key and brings it online.
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status, device_info,
    metadata, owner_id, legacy_daemon_id, visibility, custom_name, max_concurrent_tasks
)
SELECT @organization_id, r.daemon_id, r.name, r.runtime_mode, r.provider, 'offline', r.device_info,
       r.metadata, r.owner_id, r.legacy_daemon_id, r.visibility, r.custom_name, r.max_concurrent_tasks
FROM agent_runtime r
WHERE r.id = @source_runtime_id AND r.daemon_id IS NOT NULL AND r.profile_id IS NULL
ON CONFLICT (workspace_id, daemon_id, provider) WHERE profile_id IS NULL
DO UPDATE SET daemon_id = EXCLUDED.daemon_id
RETURNING *;

-- name: EnsureOrganizationProfileRuntime :one
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status, device_info,
    metadata, owner_id, legacy_daemon_id, visibility, custom_name, max_concurrent_tasks,
    profile_id
)
SELECT @organization_id, r.daemon_id, r.name, r.runtime_mode, r.provider, 'offline', r.device_info,
       r.metadata, r.owner_id, r.legacy_daemon_id, r.visibility, r.custom_name, r.max_concurrent_tasks,
       @organization_profile_id
FROM agent_runtime r
WHERE r.id = @source_runtime_id AND r.daemon_id IS NOT NULL
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET daemon_id = EXCLUDED.daemon_id
RETURNING *;

-- name: CreateOrganizationAgentCopy :one
-- Copies a project agent into the organization with its full configuration,
-- secrets included: the organization agent becomes the one that is edited.
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode, runtime_config,
    runtime_id, visibility, max_concurrent_tasks, owner_id, instructions,
    custom_env, custom_args, mcp_config, model, thinking_level,
    composio_toolkit_allowlist, permission_mode, disabled_runtime_skills,
    service_tier, conversation_starters, intake_can, intake_cannot, org_unit_id,
    is_primary
)
SELECT
    @organization_id, @name, a.description, a.avatar_url, a.runtime_mode, a.runtime_config,
    sqlc.narg('runtime_id')::uuid, a.visibility, a.max_concurrent_tasks, a.owner_id, a.instructions,
    a.custom_env, a.custom_args, a.mcp_config, a.model, a.thinking_level,
    a.composio_toolkit_allowlist, a.permission_mode, a.disabled_runtime_skills,
    a.service_tier, a.conversation_starters, a.intake_can, a.intake_cannot,
    -- Runs after the unit remap: a placement that already points into the
    -- organization tree is kept, and a primary twin otherwise follows its
    -- owner's roster placement.
    COALESCE(
        (SELECT u.id FROM org_unit u WHERE u.id = a.org_unit_id AND u.workspace_id = @organization_id),
        CASE WHEN a.is_primary THEN (
            SELECT m.org_unit_id FROM member m
            WHERE m.workspace_id = @organization_id AND m.user_id = a.owner_id
        ) END
    ),
    a.is_primary AND NOT EXISTS (
        SELECT 1 FROM agent p
        WHERE p.workspace_id = @organization_id
          AND p.owner_id = a.owner_id
          AND p.kind = 'user'
          AND p.archived_at IS NULL
          AND p.is_primary
    )
FROM agent a
WHERE a.id = @source_id
RETURNING *;

-- name: CopyAgentInvocationTargets :execrows
-- Workspace-wide targets point at the agent's own workspace, so they follow it
-- into the organization; member targets keep the same people.
INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
SELECT @target_agent_id,
       t.target_type,
       CASE WHEN t.target_type = 'workspace' THEN @organization_id::uuid ELSE t.target_id END,
       t.created_by
FROM agent_invocation_target t
WHERE t.agent_id = @source_agent_id
ON CONFLICT (agent_id, target_type, target_id) DO NOTHING;

-- name: LinkAgentToSource :execrows
UPDATE agent SET source_agent_id = @source_agent_id, updated_at = now()
WHERE id = @id AND source_agent_id IS NULL;

-- name: EnsureOrganizationMember :exec
-- Agent owners must sit on the organization roster. Like directory sync, the
-- row carries no seat intent.
INSERT INTO member (workspace_id, user_id, role)
VALUES (@organization_id, @user_id, 'member')
ON CONFLICT (workspace_id, user_id) DO NOTHING;
