-- Personal dispatches target a squad instead of a department (migration 656).
ALTER TABLE dispatch_origin ALTER COLUMN target_unit_id DROP NOT NULL;
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS target_squad_id uuid;

INSERT INTO schema_migrations (version) VALUES ('013_dispatch_personal');
