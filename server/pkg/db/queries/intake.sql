-- name: CreateIntakeAgent :one
INSERT INTO agent (
    workspace_id, name, runtime_mode, owner_id, intake_can, intake_cannot
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpdateIntakeAgentIdentity :one
UPDATE agent
SET name = $3,
    runtime_mode = $4,
    intake_can = $5,
    intake_cannot = $6,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND owner_id = $7
  AND archived_at IS NULL
  AND kind = 'user'
RETURNING *;

-- name: ListIntakeAgentsForOwner :many
SELECT
    a.id,
    a.name,
    a.owner_id,
    a.runtime_mode,
    a.intake_can,
    a.intake_cannot,
    (
        r.id IS NOT NULL
        AND r.status = 'online'
        AND COALESCE(r.last_seen_at, r.updated_at) >= now() - make_interval(secs => @runtime_stale_secs::double precision)
    ) AS online,
    (
        SELECT count(*)::int FROM agent_task_queue t
        WHERE t.agent_id = a.id AND t.status IN ('queued', 'dispatched', 'running')
    ) AS active_count
FROM agent a
LEFT JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1
  AND a.owner_id = $2
  AND a.archived_at IS NULL
  AND a.kind = 'user'
ORDER BY a.created_at ASC, a.id ASC;

-- name: ListIntakeModulesForCaller :many
SELECT m.id, m.project_id, m.product, m.module, m.scope, m.agent_id, m.backup_agent_id
FROM intake_module m
JOIN project p ON p.id = m.project_id AND p.workspace_id = m.workspace_id
WHERE m.workspace_id = $1
  AND p.lead_type = 'member'
  AND p.lead_id IS NOT NULL
  AND (
    p.lead_id = $2
    OR EXISTS (
      SELECT 1 FROM intake_project_member mem
      WHERE mem.workspace_id = m.workspace_id
        AND mem.project_id = m.project_id
        AND mem.user_id = $2
    )
  )
ORDER BY m.project_id, m.position, m.id;

-- name: GetIntakeRequestByClient :one
SELECT id, issue_id, module_id, source_body
FROM intake_request
WHERE workspace_id = $1 AND requester_id = $2 AND client_request_id = $3;

-- name: CreateIntakeRequest :one
INSERT INTO intake_request (
    workspace_id, issue_id, project_id, module_id, requester_id, client_request_id, source_body, start_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING id, issue_id;

-- name: SetIntakeIssueTodo :exec
UPDATE issue
SET status = 'todo', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'backlog';

-- name: DeleteIntakeModulesForProject :exec
DELETE FROM intake_module WHERE workspace_id = $1 AND project_id = $2;

-- name: InsertIntakeModule :exec
INSERT INTO intake_module (
    workspace_id, project_id, product, module, scope, agent_id, backup_agent_id, position
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
);

-- name: DeleteIntakeProjectMembers :exec
DELETE FROM intake_project_member WHERE workspace_id = $1 AND project_id = $2;

-- name: InsertIntakeProjectMember :exec
INSERT INTO intake_project_member (workspace_id, project_id, user_id)
VALUES ($1, $2, $3);

-- name: GetIntakeRequestByIssue :one
SELECT id, issue_id, project_id, module_id, requester_id, source_body, start_at, unit_log, redispatch_used, progress_done, progress_remain
FROM intake_request
WHERE workspace_id = $1 AND issue_id = $2;

-- name: AddIntakeRetryPass :one
UPDATE intake_request SET retry_passes = retry_passes + 1 WHERE id = $1 RETURNING retry_passes;

-- name: ResetIntakeRetryPass :exec
UPDATE intake_request SET retry_passes = 0 WHERE id = $1;

-- name: MarkIntakeRedispatch :exec
UPDATE intake_request SET redispatch_used = true, retry_passes = 0 WHERE id = $1;

-- name: GetIntakeModuleByID :one
SELECT id, project_id, agent_id, backup_agent_id
FROM intake_module
WHERE workspace_id = $1 AND id = $2;

-- name: InsertIntakeBlocker :one
INSERT INTO intake_blocker (
    workspace_id, issue_id, task_id, agent_id, done_text, missing_text, consequence_text, state
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'open'
)
RETURNING id;

-- name: CountContinuedIntakeBlockers :one
SELECT count(*)::int FROM intake_blocker WHERE issue_id = $1 AND state = 'continued';

-- name: GetOpenIntakeBlocker :one
SELECT id, done_text, missing_text, consequence_text, state
FROM intake_blocker
WHERE issue_id = $1 AND state = 'open';

-- name: CloseIntakeBlocker :exec
UPDATE intake_blocker
SET state = $2, decided_by = $3, decision_comment_id = $4, decided_at = now()
WHERE id = $1 AND state = 'open';

-- name: SetIntakeIssueStatus :exec
UPDATE issue
SET status = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: SetIntakeAssignee :exec
UPDATE issue
SET assignee_type = 'agent', assignee_id = $3, status = 'todo', updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: ListIntakeStartNotices :many
SELECT r.id, r.workspace_id, r.requester_id, r.source_body, r.start_at, r.issue_id, i.assignee_id, t.started_at
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
JOIN agent_task_queue t ON t.issue_id = r.issue_id AND t.agent_id = i.assignee_id AND t.status = 'running'
WHERE r.start_at IS NOT NULL
  AND t.attempt <= 1
  AND (r.start_notice_agent_id IS NULL OR r.start_notice_agent_id <> i.assignee_id)
ORDER BY t.started_at
LIMIT 20;

-- name: SetIntakeStartNotice :exec
UPDATE intake_request SET start_notice_agent_id = $2 WHERE id = $1;

-- name: LatestIntakeTaskLine :one
SELECT content FROM task_message tm
JOIN agent_task_queue t ON t.id = tm.task_id
WHERE t.issue_id = $1 AND tm.content IS NOT NULL AND btrim(tm.content) <> ''
  AND tm.type IN ('assistant', 'text', 'result')
ORDER BY tm.created_at DESC
LIMIT 1;

-- name: AddIntakeUnitSwitch :exec
UPDATE intake_request
SET unit_switches = unit_switches + 1,
    unit_log = CASE WHEN unit_log = '' THEN $2 ELSE unit_log || E'\n' || $2 END
WHERE id = $1;

-- name: ListIntakeReadyForReview :many
SELECT r.id, r.workspace_id, r.requester_id, r.project_id, r.issue_id, r.source_body, r.progress_done
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
WHERE i.status = 'in_review'
  AND COALESCE(r.progress_remain, '') <> '还没有测试环境'
  AND COALESCE(r.progress_remain, '') NOT LIKE '测试环境：%'
ORDER BY r.created_at
LIMIT 20;

-- name: ListIntakeProgressCandidates :many
SELECT r.id, r.issue_id, r.workspace_id, r.requester_id, r.created_at, r.start_at, r.last_user_notice_at,
       r.progress_done, r.progress_remain, i.status,
       (
         SELECT t.status FROM agent_task_queue t
         WHERE t.issue_id = r.issue_id AND t.status IN ('queued', 'dispatched', 'running')
         ORDER BY t.created_at DESC
         LIMIT 1
       ) AS run_status,
       (
         SELECT tm.content FROM task_message tm
         JOIN agent_task_queue t ON t.id = tm.task_id
         WHERE t.issue_id = r.issue_id AND tm.content IS NOT NULL
         ORDER BY tm.created_at DESC
         LIMIT 1
       ) AS latest_line,
       r.source_body
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
WHERE i.status IN ('todo', 'in_progress')
  AND COALESCE(r.progress_remain, '') <> '执行没有进展'
  AND NOT EXISTS (
    SELECT 1 FROM intake_blocker b WHERE b.issue_id = r.issue_id AND b.state = 'open'
  )
ORDER BY r.created_at
LIMIT 100;

-- name: TouchIntakeNotice :exec
UPDATE intake_request SET last_user_notice_at = now() WHERE id = $1;

-- name: ClaimIntakeNotice :execrows
UPDATE intake_request
SET last_user_notice_at = sqlc.arg(next_notice_at)
WHERE id = sqlc.arg(id) AND last_user_notice_at IS NOT DISTINCT FROM sqlc.arg(previous_notice_at);

-- name: ListStaleOpenIntakeBlockers :many
SELECT b.id AS blocker_id, r.id AS request_id, b.issue_id, b.workspace_id, r.requester_id, r.source_body, i.assignee_id
FROM intake_blocker b
JOIN issue i ON i.id = b.issue_id AND i.workspace_id = b.workspace_id
JOIN intake_request r ON r.issue_id = b.issue_id AND r.workspace_id = b.workspace_id
WHERE b.state = 'open' AND b.created_at <= now() - interval '15 minutes'
  AND NOT EXISTS (
    SELECT 1 FROM intake_blocker prev
    WHERE prev.issue_id = b.issue_id AND prev.state = 'continued'
  )
ORDER BY b.created_at
LIMIT 50;

-- name: ListIntakeModulesForProject :many
SELECT id, product, module, scope, agent_id, backup_agent_id, position
FROM intake_module
WHERE workspace_id = $1 AND project_id = $2
ORDER BY position ASC, id ASC;

-- name: ListMyScheduledIntake :many
SELECT r.issue_id, r.source_body, r.start_at
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
WHERE r.workspace_id = $1 AND r.requester_id = $2
  AND r.start_at IS NOT NULL AND r.start_at > now()
  AND i.status IN ('todo', 'backlog')
ORDER BY r.start_at ASC, r.id ASC;

-- name: ListMyIntakeRequests :many
SELECT r.issue_id, r.source_body, r.progress_done, r.progress_remain, r.start_at, i.status,
       b.done_text, b.missing_text, b.consequence_text,
       run.status AS run_status, run.started_at AS run_started_at, run.dispatched_at AS run_dispatched_at,
       i.number, p.id AS project_id, p.title AS project_title, m.id AS module_id, m.product, m.module,
       COALESCE(
         rt.status = 'online'
         AND COALESCE(rt.last_seen_at, rt.updated_at) >= now() - interval '150 seconds',
         false
       )::boolean AS runtime_online,
       r.unit_switches,
       b.created_at AS blocker_at
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
JOIN project p ON p.id = r.project_id AND p.workspace_id = r.workspace_id
JOIN intake_module m ON m.id = r.module_id AND m.workspace_id = r.workspace_id
LEFT JOIN intake_blocker b ON b.issue_id = r.issue_id AND b.state = 'open'
LEFT JOIN agent ag ON ag.id = i.assignee_id AND i.assignee_type = 'agent'
LEFT JOIN agent_runtime rt ON rt.id = ag.runtime_id
LEFT JOIN LATERAL (
  SELECT t.status, t.started_at, t.dispatched_at
  FROM agent_task_queue t
  WHERE t.issue_id = r.issue_id AND t.status IN ('queued', 'dispatched', 'running')
  ORDER BY t.created_at DESC
  LIMIT 1
) run ON true
WHERE r.workspace_id = $1 AND r.requester_id = $2 AND i.status <> 'cancelled'
ORDER BY r.created_at ASC, r.id ASC;

-- name: ListVisibleIntakeRequests :many
SELECT r.issue_id, r.source_body, r.progress_done, r.progress_remain, r.start_at, r.requester_id,
       i.status, p.id AS project_id, p.title AS project_title, p.lead_id,
       b.done_text, b.missing_text, b.consequence_text, b.created_at AS blocker_at,
       a.name AS assignee_name, a.id AS agent_id,
       run.status AS run_status, run.started_at AS run_started_at, run.dispatched_at AS run_dispatched_at,
       i.number, m.id AS module_id, m.product, m.module,
       COALESCE(
         rt.status = 'online'
         AND COALESCE(rt.last_seen_at, rt.updated_at) >= now() - interval '150 seconds',
         false
       )::boolean AS runtime_online,
       r.unit_switches, r.unit_log
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
JOIN project p ON p.id = i.project_id AND p.workspace_id = i.workspace_id
JOIN intake_module m ON m.id = r.module_id AND m.workspace_id = r.workspace_id
LEFT JOIN intake_blocker b ON b.issue_id = r.issue_id AND b.state = 'open'
LEFT JOIN agent a ON a.id = i.assignee_id AND i.assignee_type = 'agent'
LEFT JOIN agent_runtime rt ON rt.id = a.runtime_id
LEFT JOIN LATERAL (
  SELECT t.status, t.started_at, t.dispatched_at
  FROM agent_task_queue t
  WHERE t.issue_id = r.issue_id AND t.status IN ('queued', 'dispatched', 'running')
  ORDER BY t.created_at DESC
  LIMIT 1
) run ON true
WHERE r.workspace_id = $1
  AND i.status <> 'cancelled'
  AND (
    $3::boolean
    OR p.lead_id = $2
    OR EXISTS (
      SELECT 1 FROM intake_project_member mem
      WHERE mem.workspace_id = r.workspace_id
        AND mem.project_id = p.id
        AND mem.user_id = $2
    )
  )
ORDER BY p.title ASC, r.created_at ASC, r.id ASC;

-- name: SetIntakeProgress :exec
UPDATE intake_request
SET progress_done = $2, progress_remain = $3
WHERE id = $1;

-- name: FindLarkOpenIDForUser :one
SELECT b.lark_open_id, b.installation_id
FROM lark_user_binding b
JOIN lark_installation i ON i.id = b.installation_id AND i.workspace_id = b.workspace_id
WHERE b.workspace_id = $1 AND b.multica_user_id = $2 AND i.status = 'active'
ORDER BY b.bound_at DESC
LIMIT 1;

-- name: GetActiveIntakeTask :one
SELECT id, status
FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListIntakeAgentsByRuntimes :many
SELECT a.id, a.name, a.owner_id, a.runtime_id, a.workspace_id
FROM agent a
WHERE a.runtime_id = ANY($1::uuid[])
  AND a.archived_at IS NULL
  AND a.intake_can IS NOT NULL
  AND btrim(a.intake_can) <> '';

-- name: ListIntakeLeadsForAgents :many
SELECT m.agent_id, p.lead_id
FROM intake_module m
JOIN project p ON p.id = m.project_id AND p.workspace_id = m.workspace_id
WHERE m.agent_id = ANY($1::uuid[])
  AND p.lead_type = 'member'
  AND p.lead_id IS NOT NULL
UNION
SELECT m.backup_agent_id, p.lead_id
FROM intake_module m
JOIN project p ON p.id = m.project_id AND p.workspace_id = m.workspace_id
WHERE m.backup_agent_id = ANY($1::uuid[])
  AND p.lead_type = 'member'
  AND p.lead_id IS NOT NULL;

-- name: ListIntakeRequestsForAgents :many
SELECT r.issue_id, r.requester_id, r.source_body, r.workspace_id, i.assignee_id, i.status
FROM intake_request r
JOIN issue i ON i.id = r.issue_id AND i.workspace_id = r.workspace_id
WHERE i.assignee_type = 'agent'
  AND i.assignee_id = ANY($1::uuid[])
  AND i.status IN ('todo', 'backlog', 'in_progress', 'blocked');

-- name: ListIntakePlacements :many
SELECT m.agent_id, m.backup_agent_id, m.product, m.module, p.title
FROM intake_module m
JOIN project p ON p.id = m.project_id AND p.workspace_id = m.workspace_id
WHERE m.workspace_id = $1
  AND (m.agent_id = ANY($2::uuid[]) OR m.backup_agent_id = ANY($2::uuid[]));

-- name: ListIntakeAssignableAgents :many
SELECT a.id, a.name, a.intake_can,
    (
        r.id IS NOT NULL
        AND r.status = 'online'
        AND COALESCE(r.last_seen_at, r.updated_at) >= now() - make_interval(secs => $2::double precision)
    ) AS online,
    (
        SELECT count(*)::int FROM agent_task_queue t
        WHERE t.agent_id = a.id AND t.status IN ('queued', 'dispatched', 'running')
    ) AS active_count
FROM agent a
LEFT JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1
  AND a.archived_at IS NULL
  AND a.kind = 'user'
  AND a.intake_can IS NOT NULL
  AND btrim(a.intake_can) <> ''
ORDER BY a.name ASC, a.id ASC;

-- name: ListIntakeMaintainProjects :many
SELECT p.id, p.title
FROM project p
WHERE p.workspace_id = $1
  AND p.lead_type = 'member'
  AND p.lead_id IS NOT NULL
  AND ($2::text IN ('owner', 'admin') OR p.lead_id = $3)
ORDER BY p.title ASC, p.id ASC;

-- name: IntakeProjectHasMember :one
SELECT EXISTS (
  SELECT 1 FROM intake_project_member
  WHERE workspace_id = $1 AND project_id = $2 AND user_id = $3
);

-- name: CountIntakeLeadProjects :one
SELECT count(*)::int FROM project
WHERE workspace_id = $1 AND lead_type = 'member' AND lead_id = $2;

-- name: DisableIntakeWakeups :exec
UPDATE issue_wakeup
SET enabled = false, disabled_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE issue_id = $1 AND workspace_id = $2 AND enabled;
