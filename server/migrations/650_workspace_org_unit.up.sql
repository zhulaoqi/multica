-- A workspace is a project owned by one department. org_unit_id points at the
-- department's root inside this workspace's copied organisation tree; its
-- subtree is the project's member scope. NULL keeps the legacy flat behaviour.
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS org_unit_id uuid;

-- Infer the owning department from the earliest placed member, preferring an
-- owner (the department lead is usually the project owner). Only placements that
-- belong to this workspace's own tree are considered.
WITH candidate AS (
    SELECT w.id AS workspace_id, m.org_unit_id,
           ROW_NUMBER() OVER (
               PARTITION BY w.id
               ORDER BY (m.role = 'owner') DESC, m.created_at ASC, m.id ASC
           ) AS rn
    FROM workspace w
    JOIN member m ON m.workspace_id = w.id
    JOIN org_unit u ON u.id = m.org_unit_id AND u.workspace_id = w.id
    WHERE w.org_unit_id IS NULL AND m.org_unit_id IS NOT NULL
)
UPDATE workspace w
SET org_unit_id = c.org_unit_id
FROM candidate c
WHERE c.workspace_id = w.id AND c.rn = 1;

-- Owners and admins may sit outside the owning department, but a missing
-- placement would leave them outside the subtree too. Pin them to the project
-- root so they keep access. Plain members stay unplaced until the cleanup
-- command removes them.
UPDATE member m
SET org_unit_id = w.org_unit_id
FROM workspace w
WHERE m.workspace_id = w.id
  AND w.org_unit_id IS NOT NULL
  AND m.org_unit_id IS NULL
  AND m.role IN ('owner', 'admin');

-- The department lead must own the project. Insert a real member row when the
-- lead is a known user, then promote an existing row.
WITH owned AS (
    SELECT w.id AS workspace_id, u.id AS unit_id, u.lead_user_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id AND u.workspace_id = w.id
    WHERE w.org_unit_id IS NOT NULL AND u.lead_user_id IS NOT NULL
)
INSERT INTO member (workspace_id, user_id, role, org_unit_id)
SELECT o.workspace_id, o.lead_user_id, 'owner', o.unit_id
FROM owned o
JOIN "user" lead_user ON lead_user.id = o.lead_user_id
WHERE NOT EXISTS (
    SELECT 1 FROM member m
    WHERE m.workspace_id = o.workspace_id AND m.user_id = o.lead_user_id
);

WITH owned AS (
    SELECT w.id AS workspace_id, u.id AS unit_id, u.lead_user_id
    FROM workspace w
    JOIN org_unit u ON u.id = w.org_unit_id AND u.workspace_id = w.id
    WHERE w.org_unit_id IS NOT NULL AND u.lead_user_id IS NOT NULL
)
UPDATE member m
SET role = 'owner', org_unit_id = o.unit_id
FROM owned o
WHERE m.workspace_id = o.workspace_id
  AND m.user_id = o.lead_user_id
  AND (m.role <> 'owner' OR m.org_unit_id IS DISTINCT FROM o.unit_id);
