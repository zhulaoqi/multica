-- Organization-wide IM installation (migration 658). The organization bot is
-- deployment-scoped, so it cannot reuse channel_installation's
-- (workspace_id, agent_id) key. Its app credentials stay in deployment env.
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

INSERT INTO schema_migrations (version) VALUES ('015_org_channel_installation');
