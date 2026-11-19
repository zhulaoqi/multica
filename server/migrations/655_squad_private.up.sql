-- A personal squad decomposes its creator's own work. The API hides it from
-- every other member, including workspace admins, so the flag is the only
-- source of that visibility rule.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS is_private boolean NOT NULL DEFAULT false;
