-- 仅在 rag-v2 连续激活满 14 天后清理旧 chunks。
BEGIN;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM rag_index_versions
         WHERE version = 'rag-v2' AND status = 'active'
           AND activated_at <= now() - interval '14 days'
    ) THEN
        RAISE EXCEPTION 'rag-v2 has not been active for 14 days';
    END IF;
END $$;
DELETE FROM material_chunks WHERE index_version = 'legacy-v1';
UPDATE rag_index_versions SET status = 'retired' WHERE version = 'legacy-v1';
COMMIT;
