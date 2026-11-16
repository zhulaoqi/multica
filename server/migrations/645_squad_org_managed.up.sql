-- Department squads generated from the org page mirror their unit subtree.
-- Squads a human attached by hand keep org_managed = false so placement
-- changes never rewrite their roster.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS org_managed boolean NOT NULL DEFAULT false;
