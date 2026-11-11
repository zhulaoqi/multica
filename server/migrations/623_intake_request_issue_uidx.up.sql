CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_request_issue_uidx
    ON intake_request (workspace_id, issue_id);
