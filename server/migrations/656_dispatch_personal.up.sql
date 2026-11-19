-- A personal dispatch targets the requester's own squad instead of a
-- department, so the department is optional and the squad is recorded when the
-- dispatch has no unit.
ALTER TABLE dispatch_origin ALTER COLUMN target_unit_id DROP NOT NULL;
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS target_squad_id uuid;
