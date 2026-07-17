-- 评测达标后执行：验证影子索引完整，再按稳定资料 cohort 灰度到 10%/50%/100%。
BEGIN;
CREATE TEMP TABLE rag_rollout_target ON COMMIT DROP AS
SELECT :'rollout_percentage'::integer AS percentage;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM rag_rollout_target WHERE percentage IN (10, 50, 100)
    ) THEN
        RAISE EXCEPTION 'rollout_percentage must be 10, 50, or 100';
    END IF;
    IF EXISTS (
        SELECT 1 FROM materials AS m
         WHERE m.parse_status <> 'done'
            OR NOT EXISTS (
                SELECT 1 FROM material_chunks AS c
                 WHERE c.material_id = m.id
                   AND c.index_version = 'rag-v2'
                   AND c.kind = 'body'
            )
    ) THEN
        RAISE EXCEPTION 'rag-v2 is incomplete; activation aborted';
    END IF;
END $$;

DO $$
DECLARE
    target integer := (SELECT percentage FROM rag_rollout_target);
BEGIN
    IF target = 100 THEN
        UPDATE rag_index_versions
           SET status = 'retired'
         WHERE status = 'active' AND version <> 'rag-v2';
        UPDATE rag_index_versions
           SET status = 'active', activated_at = now()
         WHERE version = 'rag-v2';
    ELSE
        UPDATE rag_index_versions
           SET status = 'building', activated_at = NULL
         WHERE version = 'rag-v2';
        UPDATE rag_index_versions
           SET status = 'active', activated_at = COALESCE(activated_at, now())
         WHERE version = 'legacy-v1';
    END IF;
END $$;

UPDATE materials AS m
   SET index_version = CASE
       WHEN rollout.percentage = 100
         OR ((hashtextextended(m.id::text, 0) & 9223372036854775807) % 100)
             < rollout.percentage
       THEN 'rag-v2'
       WHEN EXISTS (
           SELECT 1 FROM material_chunks AS legacy
            WHERE legacy.material_id = m.id
              AND legacy.index_version = 'legacy-v1'
              AND legacy.kind = 'body'
       )
       THEN 'legacy-v1'
       ELSE 'rag-v2'
   END
  FROM rag_rollout_target AS rollout;
COMMIT;
