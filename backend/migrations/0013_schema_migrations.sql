-- 0013: 记录已执行迁移，供部署校验、审计和后续回滚工具使用。
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(120) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
