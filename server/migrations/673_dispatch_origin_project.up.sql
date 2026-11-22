-- Which project a dispatch was aimed at. The work still lands on the
-- project's owning department; the column lets the requester's history and
-- thread say what the request was about.
ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS target_project_id uuid;
