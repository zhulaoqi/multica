CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_module_product_uidx
    ON intake_module (workspace_id, project_id, product, module);
