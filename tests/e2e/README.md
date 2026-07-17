# 端到端验收脚本（P0 主流程 + R2 权限隔离）

这些脚本覆盖真实 HTTP 主流程、权限隔离和数据库迁移，用于上线前回归。

## 前置条件
1. PostgreSQL 17 本机运行，库 `learning_buddy` 已应用迁移（见 `backend/migrations/000*.sql`）。
   - 连接串默认 `postgres://postgres:postgres@localhost:5432/learning_buddy`
2. Agent 服务已启动并监听 `:8000`：
   - `cd agent && uvicorn main:app --host 127.0.0.1 --port 8000`
3. 后端已启动并监听 `:8080`：
   - `cd backend && go run .`

## 运行
```bash
# 全链路 P0 验收（注册/建组/加入审批/上传/解析/答疑SSE/计划/测评/记录/看板/笔记/R2）
bash tests/e2e/e2e.sh

# 单独的 R2 权限隔离验证：成员可见 shared 资料，非成员 citations=0
bash tests/e2e/r2.sh

# 在临时数据库验证全量迁移及 0007 历史题目所有者回填/清理
bash tests/e2e/migrations.sh
```

若本机 pgvector 安装文件已损坏、但已有安装好扩展的业务库，可用
`MIGRATION_TEST_DB=learning_buddy bash tests/e2e/migrations.sh`；脚本只在该库新建并最终删除一个隔离 schema，不修改已有业务表。

## 预期结论
- `e2e.sh`：脚本启用 `set -euo pipefail`；`[0.5]` 必须拒绝无服务凭证直调 Agent（HTTP 401），其余业务步骤均返回 2xx；`[7]` 必须轮询到 `ParseStatus=done`，`[8]` 必须满足成员 `citations > 0`，`[8.5]` 必须拒绝 query token（HTTP 401），并在 `[13]` 断言非成员 `citations = 0`。
- `r2.sh` 输出应为：
  ```
  成员 student   citations: 1
  非成员 student2 chat HTTP: 404
  ```
  即 **R2 权限隔离生效**：teacher 队资料仅在 `shared=true` 且用户为成员时可见，非成员携带猜测 `material_id` 的答疑/测评均返回 404。
- `migrations.sh`：在新建临时库执行 `0001`—`0006`，构造可按会话回填、可按唯一作答者回填及无法确定所有者三类历史题目，再执行 `0007`；断言前两类所有者正确、第三类及关联作答被清理、`user_id` 为 `NOT NULL`，且只存在一个指向 `users(id)` 的外键。

两个脚本都会对成员引用数量与非成员 404 做硬断言；缺少 `done` 事件、成员无引用、非成员资料 ID 请求未被拒绝或关键 Agent HTTP 异常时均以非零状态退出。

## 说明
- 所有请求走后端鉴权（JWT in `Authorization` header），SSE 用 `fetch`+`ReadableStream` 思路一致——token 不进 URL（呼应 R4）。
- 权限谓词与向量检索只在后端 repository 层实现；Agent 不提供 `/retrieve`，仅消费后端下发的已授权 chunks。R2 脚本额外断言非成员不能凭 `material_id` 生成测评。
- 无 LLM key 时，Agent 走确定性降级（`MockLLM` + 本地 `LocalEmbedder` hash trick），保证本地链路可跑通。
