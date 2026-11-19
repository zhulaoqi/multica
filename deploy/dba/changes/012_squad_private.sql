-- Personal squads (migration 655). The flag hides a squad from every member
-- except its creator.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS is_private boolean NOT NULL DEFAULT false;

INSERT INTO schema_migrations (version) VALUES ('012_squad_private');
