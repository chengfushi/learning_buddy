---
name: project-conventions
description: 智能学伴系统（learning_buddy）的项目工程规范。当在本仓库内编写、评审或修改代码（Go 后端 / React 前端 / Python Agent）时自动遵循；覆盖三栈编码规范、目录约定、权限铁律、CI 质量门禁与 PR 评审清单。
license: MIT
disable: false
---

# 项目工程规范（learning_buddy）

本仓库是一个 AI 学伴系统，技术栈：**React + Go/Gin + Python(Google ADK/A2A) + PostgreSQL/pgvector + Redis + MinIO**。
任何在本仓库内的代码工作，必须先读本规范并严格遵循。

## 0. 权限铁律（最高优先级，违反即打回）

> **凡涉及「用户可见资料范围」的逻辑，只能写在后端 `repository` 层。**
> Agent 与前端永远不直接拼权限谓词，只能消费后端下发的结果。

RAG 检索谓词（后端计算后下发）：
```sql
team_id IN (可见 team 集合) AND (teams.type <> 'teacher' OR materials.shared = true)
```
- `shared` 字段**仅对 `teacher` team 生效**；学生私有资料、public 资料的 `shared` 写入必须被忽略/拒绝。
- 任何权限相关改动**必须带测试**（单元测试覆盖谓词，集成测试覆盖完整 RAG 权限流）。

## 1. 目录约定

```
learning_buddy/
├── frontend/      # React + Vite + TypeScript（用户交互）
├── backend/       # Go + Gin + GORM（鉴权 / Team / 资料 / Agent 网关）
├── agent/         # Python + Google ADK + A2A（多智能体）
├── docs/          # 设计/PRD/数据库/工程规范文档
├── docker-compose.yml
└── .githooks/     # Git 钩子（pre-commit 等）
```

后端严格分层，依赖方向单向：`handler → service → repository`，**禁止反向依赖**：
```
backend/internal/
├── handler/    # Gin 路由与请求校验，禁止写业务/权限 SQL
├── service/    # 业务逻辑、RBAC、可见 team 计算
└── repository/ # 仅此层拼 SQL / GORM；权限谓词只在此处
```

## 2. 编码规范（三栈）

### Go（backend）
- 风格遵循 [uber-go/guide](https://github.com/uber-go/guide)；统一 `gofmt` / `goimports`。
- 静态检查：`go vet` + `staticcheck` + `golangci-lint`（见 `backend/.golangci.yml`），CI 零容忍。
- 错误处理：`fmt.Errorf("xxx: %w", err)` 包装上下文；handler 层**禁止 panic**。
- 日志用 `log/slog` 结构化；敏感字段（密码、token）脱敏。
- `context.Context` 必须透传（超时/取消/trace）。
- 关键数据路径（权限/计费）优先 `sqlc` 类型安全 SQL，减少 GORM 魔法。

### TypeScript / React（frontend）
- `tsconfig` 开 `strict: true`，**禁止 `any`**；后端响应用 `zod` 校验。
- `eslint`(typescript + react-hooks) + `prettier`，提交前 husky/pre-commit 自动跑。
- 服务端状态用 `react-query`；**SSE 用 `fetch` + `ReadableStream`**（可带 `Authorization` Header），不要用 `EventSource`（避免 token 进 URL）。
- 组件就近 co-locate；全局错误用 `ErrorBoundary` 兜底。

### Python（agent）
- `ruff`（lint + format）+ `mypy --strict`，类型零容忍。
- 所有 I/O 边界用 `pydantic v2` 模型；`asyncio` 纪律：不在同步函数内阻塞。
- 外部 HTTP 用 `httpx`，强制 timeout + 有限重试。
- 结构化日志，避免 `print`。

## 3. CI 质量门禁（PR 合入前全绿）

| 门禁 | 工具 | 不通过处理 |
|------|------|-----------|
| 格式/Lint | golangci-lint / eslint+prettier / ruff | 阻断 |
| 构建 | go build / tsc / pytest 收集 | 阻断 |
| 单测+覆盖率 | go test -cover / vitest / pytest | 整体 ≥70% 阻断；**权限/计费路径 ≥90%** 阻断 |
| 安全扫描 | gosec / bandit / semgrep | High/Critical 阻断 |
| 契约测试 | 后端↔Agent API 契约 | 阻断 |
| 架构守护 | 依赖方向 test（handler→service→repo） | 阻断 |

## 4. PR 评审清单（4-eyes 原则，≥1 资深 approval）

```
## 自评清单
- [ ] 安全：权限过滤是否仅在 repository 层？新接口是否 RBAC 校验？是否带越权测试？
- [ ] 错误：外部调用（DB/Agent/Redis/对象存储）都有 timeout/retry？无裸 panic？
- [ ] 并发：共享状态（成员关系 / shared 翻转）是否有锁或事务保护？
- [ ] 可观测：关键路径有 trace/metric/log？耗时操作有超时预算？
- [ ] 测试：新增逻辑有单测？边界/异常分支覆盖？
- [ ] 性能：是否有 N+1？向量检索是否限制 top-k？
- [ ] 文档：API / 表结构变更是否同步 docs/？
```

## 5. 提交规范

使用 Conventional Commits：`feat:` / `fix:` / `refactor:` / `test:` / `docs:` / `chore:`，scope 用服务名（`feat(backend):` / `fix(agent):`）。
提交前自动跑 `project-commit` 技能的格式化与检查（见 `.githooks/pre-commit`）。

> 完整体系见 `docs/engineering-standards.md`；库表结构见 `docs/database.md`。
