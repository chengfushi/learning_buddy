-- 异常回滚：有 legacy 数据的资料切回 legacy；v2-only 新资料保持可检索。
BEGIN;
UPDATE rag_index_versions SET status = 'building', activated_at = NULL WHERE version = 'rag-v2';
UPDATE rag_index_versions SET status = 'active', activated_at = now() WHERE version = 'legacy-v1';
UPDATE materials AS m
   SET index_version = 'legacy-v1'
 WHERE EXISTS (
       SELECT 1 FROM material_chunks AS c
        WHERE c.material_id = m.id AND c.index_version = 'legacy-v1'
   );
COMMIT;
