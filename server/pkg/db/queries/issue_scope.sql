-- Every department dispatches into the organization workspace, so its issues
-- are scoped by department instead of being visible to the whole company. The
-- read predicate itself lives in server/internal/workspaceaccess/issue_scope.go
-- because the dynamic list queries compose it; these queries feed it and the
-- realtime fan-out.

-- name: ListViewerVisibleUnits :many
-- Units whose issues the viewer reads: their own unit and its ancestors (work
-- sent to a department reaches its sub-teams), plus every unit they lead and
-- its descendants. UNION (not UNION ALL) stops a malformed cycle.
WITH RECURSIVE own AS (
    SELECT m.org_unit_id AS id
    FROM member m
    WHERE m.workspace_id = sqlc.arg(workspace_id)
      AND m.user_id = sqlc.arg(user_id)
      AND m.org_unit_id IS NOT NULL
    UNION
    SELECT parent.id
    FROM own
    JOIN org_unit child ON child.id = own.id
    JOIN org_unit parent ON parent.id = child.parent_id
),
led AS (
    SELECT u.id
    FROM org_unit u
    WHERE u.workspace_id = sqlc.arg(workspace_id)
      AND u.lead_user_id = sqlc.arg(user_id)
    UNION
    SELECT child.id
    FROM led
    JOIN org_unit child ON child.parent_id = led.id
)
SELECT own.id::uuid AS id FROM own
UNION
SELECT led.id::uuid AS id FROM led;

-- name: ListIssueAudience :many
-- Members of the issue's workspace who may read it, for per-user realtime
-- delivery. Mirrors the read predicate: owners/admins, members placed in the
-- issue's department subtree, leads of the department or an ancestor, and the
-- people directly involved. Empty when the issue has no department.
WITH RECURSIVE target AS (
    SELECT i.id, i.workspace_id, i.org_unit_id, i.creator_type, i.creator_id,
           i.assignee_type, i.assignee_id
    FROM issue i
    WHERE i.id = sqlc.arg(issue_id) AND i.org_unit_id IS NOT NULL
),
subtree AS (
    SELECT t.org_unit_id AS id FROM target t
    UNION
    SELECT child.id
    FROM subtree
    JOIN org_unit child ON child.parent_id = subtree.id
),
ancestors AS (
    SELECT t.org_unit_id AS id FROM target t
    UNION
    SELECT parent.id
    FROM ancestors a
    JOIN org_unit child ON child.id = a.id
    JOIN org_unit parent ON parent.id = child.parent_id
)
SELECT DISTINCT m.user_id
FROM target t
JOIN member m ON m.workspace_id = t.workspace_id
WHERE m.role IN ('owner', 'admin')
   OR m.org_unit_id IN (SELECT subtree.id FROM subtree)
   OR m.user_id IN (
       SELECT u.lead_user_id
       FROM ancestors a
       JOIN org_unit u ON u.id = a.id
       WHERE u.lead_user_id IS NOT NULL
   )
   OR (t.creator_type = 'member' AND t.creator_id = m.user_id)
   OR (t.assignee_type = 'member' AND t.assignee_id = m.user_id)
   OR (t.creator_type = 'agent' AND EXISTS (
       SELECT 1 FROM agent a WHERE a.id = t.creator_id AND a.owner_id = m.user_id
   ))
   OR (t.assignee_type = 'agent' AND EXISTS (
       SELECT 1 FROM agent a WHERE a.id = t.assignee_id AND a.owner_id = m.user_id
   ))
   OR (t.assignee_type = 'squad' AND (
       EXISTS (
           SELECT 1 FROM squad_member sm
           WHERE sm.squad_id = t.assignee_id
             AND sm.member_type = 'member'
             AND sm.member_id = m.user_id
       )
       OR EXISTS (
           SELECT 1 FROM squad_member sm
           JOIN agent a ON a.id = sm.member_id
           WHERE sm.squad_id = t.assignee_id
             AND sm.member_type = 'agent'
             AND a.owner_id = m.user_id
       )
       OR EXISTS (
           SELECT 1 FROM squad s
           JOIN agent a ON a.id = s.leader_id
           WHERE s.id = t.assignee_id AND a.owner_id = m.user_id
       )
   ))
   OR EXISTS (
       SELECT 1 FROM issue_subscriber s
       WHERE s.issue_id = t.id
         AND s.user_type = 'member'
         AND s.user_id = m.user_id
         AND s.unsubscribed_at IS NULL
   );
