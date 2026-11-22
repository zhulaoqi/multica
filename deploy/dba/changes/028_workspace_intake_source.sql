-- Who may hand a project work. A project with no rows follows the owning
-- department's list; with rows, this list is the only answer. Departments
-- (source_type = 'org_unit') cover their whole subtree; people are listed
-- individually (migration 671).
CREATE TABLE IF NOT EXISTS workspace_intake_source (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_intake_source_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_intake_source_type_check CHECK (source_type IN ('user', 'org_unit'))
);

INSERT INTO schema_migrations (version) VALUES ('028_workspace_intake_source');
