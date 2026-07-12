-- 对齐 BIGSERIAL 序列与已插入的显式 ID（否则后续自增会从 1 开始与种子冲突）。
-- 空表时取 1，避免 setval 越界。
SELECT setval('users_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM users), 1));
SELECT setval('teams_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM teams), 1));
SELECT setval('materials_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM materials), 1));
SELECT setval('material_chunks_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM material_chunks), 1));
SELECT setval('exercises_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM exercises), 1));
SELECT setval('study_plans_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM study_plans), 1));
SELECT setval('agent_messages_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM agent_messages), 1));
SELECT setval('quiz_attempts_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM quiz_attempts), 1));
SELECT setval('learning_records_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM learning_records), 1));
SELECT setval('material_notes_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM material_notes), 1));
SELECT setval('token_usage_id_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM token_usage), 1));
