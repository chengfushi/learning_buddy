-- 0005: 解析任务幂等保护。
-- 清理历史重复 chunk 后，强制同一资料的 chunk_idx 唯一；配合 Agent 的事务 advisory lock，
-- 防止 HTTP 超时后的旧解析线程与重试并发写入重复片段。

WITH duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY material_id, chunk_idx ORDER BY id DESC) AS row_num
    FROM material_chunks
)
DELETE FROM material_chunks
WHERE id IN (SELECT id FROM duplicates WHERE row_num > 1);

CREATE UNIQUE INDEX IF NOT EXISTS uq_material_chunks_material_idx
    ON material_chunks(material_id, chunk_idx);
