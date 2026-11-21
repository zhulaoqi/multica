-- Who may hand work to a department. Empty means only its leads and the
-- platform managers. Departments (source_type = 'org_unit') cover their whole
-- subtree; people are listed individually.
CREATE TABLE IF NOT EXISTS org_unit_intake_source (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    org_unit_id uuid NOT NULL,
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT org_unit_intake_source_pkey PRIMARY KEY (id),
    CONSTRAINT org_unit_intake_source_type_check CHECK (source_type IN ('user', 'org_unit'))
);
