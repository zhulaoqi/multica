CREATE TABLE intake_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    project_id UUID NOT NULL,
    module_id UUID NOT NULL,
    requester_id UUID NOT NULL,
    client_request_id UUID NOT NULL,
    source_body TEXT NOT NULL,
    start_at TIMESTAMPTZ,
    last_user_notice_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
