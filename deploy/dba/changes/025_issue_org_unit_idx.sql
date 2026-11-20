-- Department-scoped issue lists in the organization workspace (migration 668).
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_org_unit_idx
    ON issue (workspace_id, org_unit_id)
    WHERE org_unit_id IS NOT NULL;

INSERT INTO schema_migrations (version) VALUES ('025_issue_org_unit_idx');
