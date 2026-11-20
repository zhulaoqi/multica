-- At most one organization workspace per deployment.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_organization_uidx
    ON workspace (is_organization)
    WHERE is_organization;
