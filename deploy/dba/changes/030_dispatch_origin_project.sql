ALTER TABLE dispatch_origin ADD COLUMN IF NOT EXISTS target_project_id uuid;

INSERT INTO schema_migrations (version) VALUES ('030_dispatch_origin_project');
