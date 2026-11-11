CREATE INDEX CONCURRENTLY IF NOT EXISTS intake_request_list_idx
    ON intake_request (workspace_id, created_at DESC, id DESC);
