-- 0004: 升级 embedding 维度 768 -> 1024（接入真实 embedding 模型 text-embedding-v4）
-- 旧 material_chunks 向量为 768 维，与 1024 维不兼容，先清空再改列类型；
-- 并重置资料的解析状态，便于在切换后重新解析、写入 1024 维向量。

DELETE FROM material_chunks;

ALTER TABLE material_chunks
    ALTER COLUMN embedding TYPE vector(1024);

UPDATE materials
    SET parse_status = 'pending';
