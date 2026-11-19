-- One organization installation per channel type: the deployment runs a single
-- login-app connection for every project.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_channel_installation_type_uidx
    ON org_channel_installation (channel_type);
