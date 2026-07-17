-- 0004: 升级 embedding 维度 768 -> 1024（接入真实 embedding 模型 text-embedding-v4）
-- 旧 material_chunks 向量为 768 维，与 1024 维不兼容，先清空再改列类型；
-- 并重置资料的解析状态，便于在切换后重新解析、写入 1024 维向量。

-- 可重复执行：只在列尚未升级时清理不兼容旧向量并重置解析状态。
DO $migration$
DECLARE
    embedding_type TEXT;
BEGIN
    SELECT format_type(a.atttypid, a.atttypmod)
      INTO embedding_type
      FROM pg_attribute AS a
      JOIN pg_class AS c ON c.oid = a.attrelid
      JOIN pg_namespace AS n ON n.oid = c.relnamespace
     WHERE n.nspname = current_schema()
       AND c.relname = 'material_chunks'
       AND a.attname = 'embedding'
       AND a.attnum > 0
       AND NOT a.attisdropped;

    IF embedding_type IS DISTINCT FROM 'vector(1024)' THEN
        DELETE FROM material_chunks;
        ALTER TABLE material_chunks
            ALTER COLUMN embedding TYPE vector(1024);
        UPDATE materials
           SET parse_status = 'pending';
    END IF;
END
$migration$;
