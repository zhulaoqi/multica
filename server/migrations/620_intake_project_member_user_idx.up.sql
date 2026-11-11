CREATE INDEX CONCURRENTLY IF NOT EXISTS intake_project_member_user_idx
    ON intake_project_member (workspace_id, user_id);
