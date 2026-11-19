-- Personal dispatch rows have no department; delete them before the column
-- becomes required again so the reverse migration cannot fail on live data.
DELETE FROM dispatch_origin WHERE target_unit_id IS NULL;
ALTER TABLE dispatch_origin DROP COLUMN IF EXISTS target_squad_id;
ALTER TABLE dispatch_origin ALTER COLUMN target_unit_id SET NOT NULL;
