ALTER TABLE intake_request DROP COLUMN IF EXISTS progress_remain;
ALTER TABLE intake_request DROP COLUMN IF EXISTS progress_done;
DROP TABLE IF EXISTS feishu_identity;
