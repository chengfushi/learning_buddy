-- 重新构建 rag-v2 影子索引。Backend dispatcher 会领取 pending 任务；legacy-v1 仍在线。
BEGIN;
UPDATE rag_index_versions
   SET status = 'building', activated_at = NULL
 WHERE version = 'rag-v2' AND status <> 'active';

UPDATE materials AS m
   SET parse_status = 'pending', parse_error = NULL, parse_generation = parse_generation + 1
 WHERE NOT EXISTS (
       SELECT 1 FROM material_chunks AS c
        WHERE c.material_id = m.id AND c.index_version = 'rag-v2' AND c.kind = 'body'
   );
COMMIT;
