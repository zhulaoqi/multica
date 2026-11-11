CREATE TABLE intake_module (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    product TEXT NOT NULL,
    module TEXT NOT NULL,
    scope TEXT NOT NULL,
    agent_id UUID NOT NULL,
    backup_agent_id UUID,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
