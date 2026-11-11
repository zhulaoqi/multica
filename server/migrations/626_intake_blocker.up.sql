CREATE TABLE intake_blocker (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    task_id UUID,
    agent_id UUID,
    done_text TEXT NOT NULL,
    missing_text TEXT NOT NULL,
    consequence_text TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('open', 'continued', 'stopped')),
    decision_comment_id UUID,
    decided_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at TIMESTAMPTZ
);
