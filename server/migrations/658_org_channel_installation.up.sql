-- Organization-wide IM installation. channel_installation is keyed by
-- (workspace_id, agent_id) because a bot belongs to one agent; the organization
-- bot belongs to the deployment and serves every project, so it keeps its own
-- row. Credentials stay in deployment env (MULTICA_FEISHU_APP_ID/_SECRET);
-- config only records the non-secret routing identity for operators.
CREATE TABLE IF NOT EXISTS org_channel_installation (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type        TEXT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    ws_lease_token      TEXT,
    ws_lease_expires_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
