CREATE INDEX CONCURRENTLY IF NOT EXISTS member_org_unit_idx
    ON member (workspace_id, org_unit_id);
