-- 0012: 完整记录 AI 调用状态、延迟和估算成本，支持额度审计与异常告警。
ALTER TABLE token_usage
    ADD COLUMN IF NOT EXISTS model VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'success',
    ADD COLUMN IF NOT EXISTS latency_ms BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS estimated_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error_type VARCHAR(160);

CREATE INDEX IF NOT EXISTS idx_token_usage_user_service_created
    ON token_usage(user_id, service, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_token_usage_status_created
    ON token_usage(status, created_at DESC);
