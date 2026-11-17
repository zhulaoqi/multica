-- One dispatch per requester retry key (migration 648).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS dispatch_origin_client_uidx
    ON dispatch_origin (workspace_id, requester_id, client_request_id);

INSERT INTO schema_migrations (version) VALUES ('005_dispatch_client_uidx');
