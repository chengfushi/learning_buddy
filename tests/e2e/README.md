# 端到端验收脚本（P0 主流程 + R2 权限隔离）

这两个脚本走真实 HTTP 接口，串起「智能学伴」P0 全链路，用于上线前回归。

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
```

## 预期结论
- `e2e.sh`：每步均返回 2xx；`[7]` 轮询到 `ParseStatus=done`；`[8]` 答案含检索引用且能复现资料内容（如 `F=ma`）。
- `r2.sh` 输出应为：
  ```
  成员 student   citations: 1
  非成员 student2 citations: 0
  ```
  即 **R2 权限隔离生效**：teacher 队资料仅在 `shared=true` 且用户为成员（或公开/已批准 teacher 队）时可见，非成员无法检索到。

## 说明
- 所有请求走后端鉴权（JWT in `Authorization` header），SSE 用 `fetch`+`ReadableStream` 思路一致——token 不进 URL（呼应 R4）。
- 权限谓词只在后端 repository 层与 Agent `retrieve` 内实现（`team_id IN(可见集) AND (type<>'teacher' OR shared=true)`），Agent 仅消费后端下发的可见 team 集合，无法扩大范围。
- 无 LLM key 时，Agent 走确定性降级（`MockLLM` + 本地 `LocalEmbedder` hash trick），保证本地链路可跑通。
