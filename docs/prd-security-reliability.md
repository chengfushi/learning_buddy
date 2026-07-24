# 安全、可靠性与成本治理 PRD

## 1. 背景

当前认证、服务生命周期和 AI 调用保护存在生产风险：Access Token 有效期过长，Refresh Token 未轮换且通过 JSON 返回；前端将 Access Token 写入 `localStorage`；Agent CORS 允许任意来源并携带凭证；Agent 端口在 Compose 中暴露到宿主机；后端解析 dispatcher 没有绑定进程生命周期。项目已有 token_usage、订阅额度和 Redis 设计，但尚未形成完整的用户级限流与成本闭环。

## 2. 目标

- 降低 XSS、Refresh Token 重放、跨域调用和内部 Agent 暴露风险。
- 确保后台任务随服务退出而取消，并支持健康检查和就绪依赖。
- 为 Chat 建立用户级限流、每日额度和模型 token 用量记账闭环。
- 为后续 Plan/Quiz、API 契约治理、RAG 评测和前端请求治理提供可验证的分阶段基础。

## 3. 范围与优先级

### P0（本迭代）

1. Access Token 有效期调整为 15 分钟，并增加明确的 `typ=access` 与用途校验。
2. Refresh Token 改为 httpOnly、Secure、SameSite Cookie，不再通过 JSON 返回或从请求体读取。
3. Refresh Token 使用随机 opaque token；数据库仅保存哈希、用户、过期时间、撤销时间、轮换关系和使用时间。每次刷新必须轮换，旧 token 重放时撤销该 token 链。
4. Agent CORS 改为环境变量白名单；生产默认拒绝跨域，禁止 `* + credentials`。
5. 生产 Compose 不映射 Agent 端口；保留容器内 `8000` 供 Backend 通过服务网络访问。
6. Backend 使用 `signal.NotifyContext` 管理 dispatcher 与 HTTP Server 的优雅停机。

### P1（后续迭代）

- 共享 `httpx.AsyncClient`、外部调用超时/重试/熔断和任务并发上限。
- Chat Redis 滑动窗口限流、每日额度、失败调用记账和 Plan/Quiz 复用。
- Backend/Agent 覆盖率门禁、mypy 包结构修复和 handler→service→repository 越权测试。
- OpenAPI 单一来源、自动生成前端类型，以及统一 401 刷新流程。
- 建立不少于 100 条人工标注 RAG 集，关联 `message_feedback`、`rag_runs`、召回命中和点击行为。

## 4. API 与兼容性

- `POST /api/auth/login`、`POST /api/auth/register`：响应保留 `user` 与 `access_token`，删除 `refresh_token`；通过 `Set-Cookie` 写入 Refresh Cookie。
- `POST /api/auth/refresh`：只读取 Cookie，成功后返回新的 `access_token` 并轮换 Cookie；缺失、过期、撤销或重放返回 401。
- `POST /api/auth/logout`：撤销当前 Refresh Token 并清除 Cookie。
- 前端请求统一携带 `credentials: "include"`；Access Token 只保存在内存，不写入 Web Storage。

## 5. 验收标准

- 单元测试证明 access token 的 `typ` 和 TTL；refresh token 不能被 `VerifyToken` 接受。
- 登录/注册响应体不含 refresh token，响应包含 HttpOnly Cookie；刷新后旧 Cookie 不能再次使用。
- 刷新 token 的数据库字段只保存不可逆哈希；登出后 Cookie 失效。
- Agent CORS 测试证明白名单外来源不被允许，且生产配置不包含通配符。
- Compose 配置中 Agent 没有 `ports` 宿主机映射，Backend 仍能通过 `agent:8000` 访问。
- Backend 收到 SIGTERM 时取消 dispatcher context 并在超时内关闭 HTTP Server。
- `go test ./...`、`go vet ./...`、`ruff check .`、`mypy --strict .`（修复包冲突后）通过；新增安全路径测试覆盖率门禁。

## 6. 非目标

本迭代不更换 JWT 签名算法、不引入新的身份提供商、不重写现有 RAG 检索算法，也不把 Agent 暴露为公共 API。

## 7. 发布与回滚

先部署兼容读取阶段，再切换前端 Cookie 流程；旧 JWT Refresh Token 在迁移窗口内拒绝刷新。若 Cookie 或轮换异常，可回滚应用版本，但不得恢复生产 Agent 宿主机端口暴露或通配 CORS。
