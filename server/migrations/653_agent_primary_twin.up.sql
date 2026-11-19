-- A member may own several agents; one is their primary digital twin. The
-- partial unique index migration (654) adds the one-per-owner guarantee after
-- this backfill.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS is_primary boolean NOT NULL DEFAULT false;

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
