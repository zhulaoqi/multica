CREATE INDEX CONCURRENTLY IF NOT EXISTS intake_request_scheduled_idx
    ON intake_request (workspace_id, requester_id, start_at, id)
    WHERE start_at IS NOT NULL;
