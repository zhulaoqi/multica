-- A member may own several agents; one is their primary digital twin. The org
-- tree shows that twin on the member row and new projects clone its identity
-- (migration 653). The partial unique index in the next change enforces one
-- primary per owner per workspace.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS is_primary boolean NOT NULL DEFAULT false;

-- Preserve the pre-flag behaviour: the org tree used the oldest agent an owner
-- created. Pick that same row so existing twin placements do not move. The
-- reset keeps a re-run from leaving two primaries before the index exists.
UPDATE agent SET is_primary = false WHERE is_primary;

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY workspace_id, owner_id
               ORDER BY created_at ASC, id ASC
           ) AS rn
    FROM agent
    WHERE owner_id IS NOT NULL
      AND kind = 'user'
      AND archived_at IS NULL
)
UPDATE agent a
SET is_primary = true
FROM ranked r
WHERE a.id = r.id AND r.rn = 1;

INSERT INTO schema_migrations (version) VALUES ('010_agent_primary_twin');
