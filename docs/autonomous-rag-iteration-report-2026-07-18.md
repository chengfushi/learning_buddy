# 自主 RAG 迭代工作报告（2026-07-18）

## 1. 停止点

- 分支：`codex/autonomous-rag-iteration`
- 基线：`origin/main` / `417fc4b`
- 功能代码停止点：`a0efb0b`
- 功能迭代相对基线：19 个提交，41 个文件，新增 3102 行、删除 247 行（不含本报告自身）
- 推送状态：未推送，当前分支没有配置远端跟踪分支
- 工作区状态：生成本报告前为干净状态

本轮按“一个可独立验证的 MVP 一个提交”推进，未改写历史、未合并提交、未推送远端。

## 2. 完成内容

### 2.1 检索质量与降级

- 增加 RAG 离线评测发布门禁，覆盖 Recall@20、Rerank Recall@5、MRR、NDCG 和检索 P95；不达标时返回非零状态，避免用探索报告误激活索引。
- 将 Query Analysis 与 Embedding 缓存拆开。Embedding 超时、维度错误、布尔值、NaN 或 Infinity 只触发当次词法降级，不写 Redis，避免一次故障造成持续 30 分钟的空向量命中。
- 扩大 Query Rewrite 的指代识别范围，覆盖“这个参数”“它支持什么”“那超时时间呢”等常见追问；无历史或已经自包含的问题跳过改写。
- 严格校验 Rerank 的远端响应和缓存内容：结果必须数量完整、索引唯一、候选受限且分数有限，否则按原 RRF 顺序回退。
- 新增 `RERANK_MAX_DOCUMENT_TOKENS`，默认 4000；中文按保守 Token 估算截断，英文同时受 4 倍字符数上限约束，防止一个旧版超长块导致整次 Rerank 请求失败。
- 将 `qwen3-rerank` 示例端点更新为当前带 Workspace ID 的兼容接口格式；自建代理仍可通过环境变量覆盖。接口字段类型和单文档上限已按[百炼官方 Text Rerank API](https://help.aliyun.com/en/model-studio/text-rerank-api)核对。
- 无可引用正文时固定返回“当前知识库未找到依据”，不调用生成模型，避免依赖模型常识编造答案。
- SSE `done` 增加阶段耗时和降级阶段，前端可显示当前回答发生过哪些回退。

### 2.2 会话、SSE 与反馈一致性

- 会话增加资料作用域，已有会话必须与本次 `material_id` 一致，避免同一个 session 在不同资料之间串上下文。
- 恢复最近对话历史，但只读取当前用户和当前资料作用域内的历史，Query Rewrite 不再依赖前端自行拼接历史。
- Agent SSE 只接受合法事件；Backend 在转发 token 时及时 Flush，并将异常结束、空回答和缺失 `done` 统一转成错误事件。
- `done` 只在 assistant 消息持久化成功后发送，并携带真实 `message_id`；保存失败不会向前端伪报完成。
- 反馈接口按 `message_id` 幂等更新；前端防止重复提交、保留失败重试能力，取消点踩理由不会误提交，理由长度按 Unicode 字符而不是 UTF-8 字节判断。

### 2.3 Parser、图片与引用

- DOCX 图片改为按 XML 出现顺序提取，不再遍历无序关系表；段落图片、纯图片段落和表格图片都携带当前标题路径。
- PDF 扫描页和内嵌图片携带页码上下文。
- 图片继续按 SHA-256 去重；同一图片在多个章节出现时合并出现位置，并将章节/页码写入 image chunk 的检索上下文。
- Citation 增加 Reader 深链，支持跳转到资料页、PDF 页标题或指定图片资产；后端仍按实际送入上下文的 chunk 重建可信引用。

### 2.4 前端竞态与交互

- Library 的团队切换、资料加载、解析轮询、重试和创建请求按 team 隔离，旧请求完成后不能覆盖新团队页面。
- Reader 的正文、笔记、图片和原文件 URL 请求按 material 隔离，快速切换资料时不会显示上一份资料的晚到响应。
- Companion 保存并复用 session，展示改写查询、降级状态、可信引用和点赞/点踩控件。
- 新增引用跳转与视觉聚焦样式。

### 2.5 文档与发布素材

- 新增生产化 RAG 工程文章：`docs/blog/building-production-rag-with-pgvector.md`。
- 生成并保留三版博客封面，当前文章使用 `rag-production-cover-v3.png`。
- 同步 API、数据库、系统设计和 RAG v2 运行手册中的会话作用域、SSE、反馈、评测与降级行为。

## 3. 提交清单

| 提交 | 内容 |
|---|---|
| `2351288` | 新增生产化 RAG 工程文章 |
| `9767077` | Reader 支持 Citation 深链 |
| `5bff52d` | 增加 RAG 评测发布门禁 |
| `d4e566d` | 暴露 RAG 降级诊断信息 |
| `f5bbe06` | 校验并及时 Flush SSE 事件 |
| `6cb1739` | 会话按资料作用域隔离 |
| `84c4cc3` | 新增博客封面 |
| `92721fe` | 恢复作用域内对话历史 |
| `82281bd` | Reader 异步请求按资料隔离 |
| `0155641` | Library 异步请求按团队隔离 |
| `b13eb8b` | 无证据时拒答且不调用模型 |
| `fe5cf41` | assistant 持久化成功后再发送 SSE done |
| `e372cfc` | 加固回答反馈提交 |
| `e4e3d57` | 更新博客封面至 v3 |
| `b2bb698` | 保留文档图片顺序和章节上下文 |
| `1b1381b` | 避免缓存降级后的空 Embedding |
| `953196e` | 使用前严格校验 Rerank 结果 |
| `2d7470d` | 扩展上下文追问识别 |
| `a0efb0b` | 限制 Rerank 单文档输入 |

## 4. 验证结果

### 4.1 当前停止点已复验

- Agent：`ruff format --check .` 通过。
- Agent：`ruff check .` 通过。
- Agent：排除数据库集成文件 `tests/test_retrieve.py` 后，83 个测试通过。
- 最新提交钩子：`gofmt`、`go vet ./...`、`golangci-lint run` 通过。
- 最新提交钩子：Frontend Prettier 与 ESLint 通过。
- 最新提交钩子：Agent Ruff 通过。
- 所有提交均通过仓库提交钩子后写入历史。

### 4.2 未验证或需要真实环境验证

- 本机 `localhost:5432` 当前不可达，因此未把数据库集成测试标记为通过；`tests/test_retrieve.py` 仍需在迁移完成的 PostgreSQL/pgvector 实例上运行。
- 未配置真实百炼、DeepSeek、MinIO 和 OCR/VL 凭证，本轮外部协议通过 mock/契约测试验证，没有声称真实云端 E2E 通过。
- 尚未提供 100/300 条人工标注评测集，因此 Recall、MRR、NDCG 和 P95 只有发布门禁实现，没有生产数据结果。
- 分支尚未推送，远端 CI 尚未执行。
- 测试仍显示 FastAPI `on_event`、Starlette TestClient 和 PyMuPDF SWIG 的弃用警告；不影响当前用例通过，但需要后续依赖升级处理。

## 5. 后续建议

1. 启动 PostgreSQL/pgvector，执行全部迁移和 Agent/Backend 数据库集成测试。
2. 用真实 MinIO 完成 DOCX/PDF 上传、解析、图片签名 URL、引用跳转和反馈入库 E2E。
3. 配置实际 Workspace ID 和模型凭证，对 Query Rewrite、Embedding、Rerank、OCR/VL、DeepSeek 分别做超时与协议验收。
4. 补充 Backend 对原始问题和 Rerank query 的输入长度上限；当前仅限制候选文档输入。
5. 将人工标注集扩展到至少 100 条，生产切换前达到 300 条并运行 `evaluate.py` 发布门禁。
6. 用户确认后推送 `codex/autonomous-rag-iteration`，再由远端 CI 做最终验收。
