-- One directory row per person per workspace (migration 652).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_directory_member_uidx
    ON workspace_directory_member (workspace_id, user_id);

INSERT INTO schema_migrations (version) VALUES ('009_workspace_directory_member_uidx');
