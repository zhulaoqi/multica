CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_blocker_open_uidx
    ON intake_blocker (issue_id) WHERE state = 'open';
