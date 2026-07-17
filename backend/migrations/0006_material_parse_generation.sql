-- 0006: 为资料解析任务增加持久化代次。
-- 每次重新入队递增 parse_generation；Backend worker 与 Agent 写入都必须匹配该代次，
-- 防止超时请求或旧 worker 覆盖新内容、误写新任务的最终状态。

ALTER TABLE materials
    ADD COLUMN IF NOT EXISTS parse_generation BIGINT NOT NULL DEFAULT 1;
