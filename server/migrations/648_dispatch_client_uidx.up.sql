CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS dispatch_origin_client_uidx
    ON dispatch_origin (workspace_id, requester_id, client_request_id);
