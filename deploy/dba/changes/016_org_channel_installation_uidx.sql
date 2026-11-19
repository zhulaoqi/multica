-- One organization installation per channel type (migration 659).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_channel_installation_type_uidx
    ON org_channel_installation (channel_type);

INSERT INTO schema_migrations (version) VALUES ('016_org_channel_installation_uidx');
