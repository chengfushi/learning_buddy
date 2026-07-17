-- 每日维护任务：RAG 明细保留 90 天；rag_run_hits 由外键级联清理。
DELETE FROM rag_runs WHERE created_at < now() - interval '90 days';
