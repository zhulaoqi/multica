CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_request_client_uidx
    ON intake_request (workspace_id, requester_id, client_request_id);
