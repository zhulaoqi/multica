CREATE INDEX CONCURRENTLY IF NOT EXISTS dispatch_origin_list_idx
    ON dispatch_origin (workspace_id, requester_id, created_at DESC);
