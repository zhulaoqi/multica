-- At most one organization workspace per deployment (migration 664).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_organization_uidx
    ON workspace (is_organization)
    WHERE is_organization;

INSERT INTO schema_migrations (version) VALUES ('021_workspace_organization_uidx');
