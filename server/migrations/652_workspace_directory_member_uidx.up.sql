CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_directory_member_uidx
    ON workspace_directory_member (workspace_id, user_id);
