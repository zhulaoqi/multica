CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS feishu_identity_open_id_uidx
    ON feishu_identity (open_id);
