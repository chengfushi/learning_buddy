# RAG v2 生产运行手册

RAG v2 保留 PostgreSQL/pgvector 和 Backend 权限真源。Agent 只解析资料、分析查询、重排已授权候选和生成回答；向量/词法候选、父文档扩展、资产访问和引用校验都由 Backend 再次应用 `VisibleMaterialsScope`。

## 数据与检索链路

上传接口同时支持 JSON 和 multipart 文件，文件上限 50 MiB。Backend 校验扩展名、MIME、UTF-8 或 DOCX/PDF 文件签名后把原件写入 `materials-source`。Parser 从最小权限账号读取原件，按 extract → clean → enrich → assets/OCR → chunk → embed → persist 记录进度，并把规范 Markdown 和 SHA 去重图片写入 `materials-derived`。

RAG v2 将正文、摘要、候选问题和图片说明分开存为 `body/summary/question/image` 信号，不向每个正文块重复拼接摘要。查询时执行向量 Top-30 与全文/Trigram Top-30，使用 RRF(k=60) 合并为 20 条；`qwen3-rerank` 保留 Top-8，失败回退 RRF。父资料扩展再次鉴权，最多选择 3 份资料、8 个正文块和 12k Token。Backend 只接受实际上下文中存在的 chunk ID，并用可信数据库字段重建引用。

## 配置与迁移

```bash
DB_DSN='postgres://...' make migrate
DB_DSN='postgres://...' PARSER_DB_PASSWORD='...' make provision-parser
```

Backend 需要分别配置 MinIO 内部连接端点与浏览器可解析的 `MINIO_PUBLIC_ENDPOINT`；Agent 生产账号应只有 source 只读和 derived 写入权限。原始图片默认不发送到远端 OCR/VL，只有在资料已确认不含敏感信息或视觉端点位于受控内网时才显式开启 `VISION_ALLOW_RAW_IMAGES`。模型、超时、MinIO 和分块参数见 `backend/.env.example`、`agent/.env.example`。日志不得记录原始查询或正文；Prometheus 从 Backend `/metrics` 和 Agent `/metrics` 抓取。

## 影子索引与上线

1. `DB_DSN=... make reindex-rag-v2` 将缺少 v2 正文块的资料重新入队；既有资料继续使用 legacy-v1。
2. 团队按 `tests/evals/rag_cases.example.jsonl` 格式维护至少 100 条人工标注，生产前扩充到 300 条。
3. 在 Agent 目录执行 `python evaluate.py ../tests/evals/rag_cases.jsonl > rag-evaluation.json`。脚本默认对 `rag-v2` 执行发布门禁：Recall@20 ≥ 0.95、Rerank Recall@5 ≥ 0.90、检索 P95 ≤ 2500ms，任一不达标即返回非零退出码；报告中的 `failed_cases` 用于回查未完全召回的标注用例。仅探索指标时可显式使用 `--report-only`，该参数不得用于激活流程。
4. 达标后依次执行 `DB_DSN=... RAG_ROLLOUT_PERCENTAGE=10 make activate-rag-v2`、`50` 和 `100`。脚本会验证每份资料都有 v2 body，并按 material ID 的稳定 cohort 切换；重复执行同一比例是幂等的，缩小比例也会把 cohort 外且存在 legacy 数据的资料切回。
5. 每个灰度阶段观察约 48 小时。异常执行 `DB_DSN=... make rollback-rag-v2`；有 legacy 的资料立即切回，新上传的 v2-only 资料保持可用。
6. v2 连续激活 14 天后，人工执行 `backend/scripts/retire_legacy_rag.sql`。每日执行 `retain_rag_runs.sql` 清理超过 90 天的运行明细。

## 公共接口变化

- `POST /api/materials`：兼容 JSON，新增 TXT/MD/DOCX/PDF multipart 上传。
- `GET /api/materials/:id/source-url`：权限校验后的短时效原件 URL。
- `GET /api/materials/:id/assets`：权限校验后的图片与 OCR 元数据。
- `GET /api/materials/:id/processing`：当前解析阶段与进度。
- `PUT /api/agent/messages/:id/feedback`：幂等点赞/点踩。
- Agent 内网新增 `/analyze-query` 与 `/rerank`。
- Chat SSE 新增 `meta`；`done` 增加 `message_id`、阶段耗时、降级阶段和增强引用，原有 `token/done/end` 保持兼容。

## 重点告警

至少关注 RAG 各阶段 P95/P99、降级次数、空召回、点踩率、解析失败率、MinIO 错误、PostgreSQL/HNSW 内存和模型 API 延迟。缓存只保存 Query Analysis、Embedding 与“候选内容哈希 + 模型版本”的 Rerank 结果，不缓存权限集合和最终答案。
