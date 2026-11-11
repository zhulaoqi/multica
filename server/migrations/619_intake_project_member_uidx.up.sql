CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_project_member_uidx
    ON intake_project_member (workspace_id, project_id, user_id);
