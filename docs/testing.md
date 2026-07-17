# 测试策略与覆盖率门禁

> 状态：持续收敛 · 基线日期：2026-07-15

本项目采用三层测试模型，并由 `.github/workflows/ci.yml` 在真实 PostgreSQL/pgvector 环境中执行。安全用例只能增加，不能删除或降级。

## 1. 测试分层

| 层级 | 目的 | 当前权威用例 | CI 失败条件 |
|------|------|--------------|-------------|
| 单元/组件 | 隔离验证纯逻辑、错误分支、超时和 UI/API 行为 | Go `*_test.go`、Agent `tests/test_*.py`、Frontend Vitest | 任一 lint、测试或构建失败 |
| 集成/安全 | 使用 PostgreSQL 验证 repository 权限真源、解析幂等、即时可见性 | `permission_test.go`、`material_visibility_test.go`、`team_flow_test.go`、`test_retrieve.py` | 任一正向或越权反向断言失败 |
| 契约/E2E | 防止 Backend↔Agent 字段漂移，并验证真实鉴权、解析、RAG 主流程 | `tests/contracts/agent_api.json`、两侧 contract test、`e2e.sh`、`r2.sh` | 任一契约、HTTP 状态或引用硬断言失败 |

共享契约样例 `tests/contracts/agent_api.json` 是 Backend→Agent HTTP 字段与典型响应的单一测试真源。Go DTO 与 FastAPI/Pydantic 必须同时通过该样例；修改 Agent API 时必须同步更新两端实现、契约样例和文档。

## 2. 本地命令

```bash
# Backend
cd backend
go vet ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out

# Agent
cd agent
ruff check .
python -m pytest tests/ -v --cov=. --cov-fail-under=70 --cov-report=term-missing

# Frontend
cd frontend
npx eslint src/
npm run coverage
npm run build

# 服务启动后
bash tests/e2e/e2e.sh
bash tests/e2e/r2.sh
```

## 3. 覆盖率门槛与基线

目标门槛：生产源代码整体 statements/lines ≥ 70%；权限、安全、认证与计费路径 ≥ 90%。覆盖率必须包含未被测试导入的源文件，排除测试代码、生成产物与虚拟环境。

2026-07-15 实测基线：

| 栈 | 整体基线 | 当前硬门禁 | 下一优先补测区域 |
|----|----------|------------|------------------|
| Backend | 32.1% statements | 报告中；认证/RBAC middleware 100%，JWT 解析与注册均 ≥ 90%，认证及 Learning HTTP 主路径已覆盖，整体尚未达到 70% | 其余 handler、repository 写路径 |
| Agent | 75.62% statements | **70%** | `llm.py` 异常与解析分支 |
| Frontend | 93.23% statements / 92.59% branches / 73.14% functions / 93.23% lines | **四维 70% 硬门禁**；所有页面 statements/lines 均达 100% | API 异常与 SSE 解析分支、入口挂载测试 |

不得通过缩小 `include`、纳入测试文件、排除低覆盖生产模块或降低阈值来“达标”。Backend 与 Frontend 达到 70% 后，CI 必须在同一改动中启用硬阈值；安全路径的 90% 使用定向 coverprofile/测试集合单独验证。

## 4. CI 执行顺序

1. 启动 PostgreSQL/pgvector，并按文件名顺序应用所有迁移。
2. 执行 Backend vet、全量测试和覆盖率报告。
3. 执行 Agent ruff、全量 pytest、契约测试和 70% 覆盖率门禁。
4. 执行 Frontend eslint、Vitest 四维 70% 覆盖率门禁和生产构建。
5. 以同一 `AGENT_SHARED_SECRET` 启动 Backend/Agent。
6. 执行完整 E2E 与 R2 权限专项；无凭证 Agent 直调、query token、非成员详情/笔记/team 列表访问及引用均为硬失败。

## 5. 安全测试纪律

- 权限谓词仅能位于 Backend repository；相关改动必须运行 `r2.sh`。
- 每条允许路径必须有对应拒绝路径，例如 owner/成员/非成员、正确/错误服务凭证、shared true/false；不可见资料与不存在统一返回 404，避免 ID 枚举。
- 不允许把硬断言改成日志输出，不允许用空召回的 `0/0` 结果判定权限测试通过。
- 新增外部调用必须测试 timeout、取消或降级分支。
- 修改迁移、HTTP schema 或认证 Header 时必须运行完整 E2E。
