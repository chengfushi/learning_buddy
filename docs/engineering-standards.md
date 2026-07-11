# 团队技术能力提升与代码质量把控手册

> 版本：v0.1 · 状态：草案 · 最后更新：2026-07-11
> 适用项目：智能学伴系统（React + Go/Gin + Python/Google ADK/A2A + PostgreSQL/pgvector + Redis）
> 目的：把「团队技术水平提升」与「代码质量把控」从口号变成可执行的工程纪律。

---

## 0. 如何使用本手册

本手册面向两类读者：

- **技术负责人 / 资深开发**：照 §1 把风险登记进技术债看板，照 §2–§5 落地工程纪律。
- **团队成员（含初级）**：把 §3 的能力分级当作成长地图，把 §2.3 的评审清单当作每次提 PR 的自检表。

**铁律（不可妥协）**：凡涉及「用户可见资料范围」的逻辑，**只能写在后端 repository 层**，Agent 与前端永远不直接拼权限谓词。违反此条视为严重缺陷。

---

## 1. 资深开发者视角的项目技术风险清单

基于 `docs/system-design.md` v0.3 评审，下列风险按严重度分级。每一项都给了「现象 / 影响 / 对策 / 验收」。

### P0（MVP 前必须闭环，否则上线即事故）

| # | 风险 | 现象 / 根因 | 影响 | 对策 | 验收 |
|---|------|------------|------|------|------|
| R1 | **Embedding 维度不一致** | `README` 写 `EMBEDDING_DIM=1536`（OpenAI 维度），但设计约定用 Google 配套 embedding（768 / 3072），两环境维度不同会导致向量检索**静默返回垃圾** | RAG 答非所问，且无报错 | ① 单一真源：维度只在配置中心定义，启动时断言与库表一致；② 建库时校验 `vector` 列维度；③ 全库统一模型 | 故意用错维度启动 → 启动失败；跨服务维度一致 |
| R2 | **RAG 权限谓词被绕过** | 检索谓词 `team_id IN(可见集) AND (teams.type<>'teacher' OR materials.shared=true)` 正确，但若重构时有人在 Agent/前端重拼，或漏掉 `shared` | 学生读到老师备课草稿（越权） | ① 谓词**只**在后端 repository 构建，Agent 仅收「过滤后的片段」；② 加**反向测试用例**：学生查询必须断言不包含 `shared=false` 的 teacher 资料；③ Code Review 清单强制卡（见 §2.3） | 安全测试红 / 绿驱动；任何越权路径有测试覆盖 |
| R3 | **解析队列不可靠** | `parse_status` 异步（`pending→parsing→done/failed`），worker 崩溃会卡在 `parsing`；重解析可能重复 chunk | 资料「一直在解析中」，知识库缺数据 | ① 解析任务设超时 + 失败重入队（指数退避）；② **幂等**：重解析先删旧 chunk 再写；③ 死信队列 + 告警；④ `failed` 前端可重试 | 模拟 worker 崩溃 → 任务自动恢复；重复触发不产生重复 chunk |

### P1（MVP 后尽快补，影响稳定性/安全）

| # | 风险 | 现象 / 根因 | 影响 | 对策 | 验收 |
|---|------|------------|------|------|------|
| R4 | **SSE 鉴权 token 泄漏** | 设计说「SSE 通过 query 参数传 access token」，`EventSource` 不能带 Header，但 query 中的 token 会进 access log | token 泄露 → 会话劫持 | ① 同域用 **httpOnly + Secure Cookie** 自动携带；② 跨域用极短时效签名 ticket；③ 日志脱敏 token | 网关日志不含明文 token；cookie 标记 httpOnly |
| R5 | **Agent 间 A2A 无认证** | Orchestrator→Retriever→Tutor 走 A2A HTTP，未提 agent 间鉴权 | 内网可达即能调 Agent | ① agent 间 mTLS 或服务间共享密钥；② 网络层隔离（仅后端可触 Agent） | 外部直接调 Agent 端口被拒 |
| R6 | **多智能体链路无超时/降级** | 答疑链路顺序调用 Retriever→Tutor，任一环慢则整体慢，无兜底 | 单次问答超时、雪崩 | ① 每跳设超时预算（如 Retriever 800ms）；② Retriever 失败**降级为无 RAG** 直接答；③ 每 agent 独立 trace | 注入 Retriever 故障 → 仍返回（降级）答案；P99 可控 |
| R7 | **缓存失效覆盖不全** | `team:visible:{user_id}` 在 `shared` 翻转 / 成员审批时失效，但任一遗漏的写路径都会脏读 | 学生最多 5 分钟看到草稿或漏看新公开资料 | ① 失效逻辑**集中**在 repository 的写方法内，不散落业务层；② 对「可见集」计算加集成测试 | 改 `shared` / 审批成员后，立即可见性生效（测试断言） |
| R8 | **缺测试策略** | 文档未定义测试分层 | 权限回归无防护，重构即碎 | 见 §2.4：单测（谓词）+ 集成（完整 RAG 权限流）+ 契约（后端↔Agent） | 三层测试在 CI 跑通，覆盖率达标 |

### P2（规模化阶段）

| # | 风险 | 对策 |
|---|------|------|
| R9 | **可观测性未落地** | 接入 OpenTelemetry，打通 Gin→Agent→pgvector 的分布式追踪，定位 RAG 延迟瓶颈 |
| R10 | **并发竞态** | 加入 team 审批、`shared` 翻转与进行中查询的竞态，用行锁 / 乐观锁兜底 |

> 将上述 10 项登记进**技术债看板**（建议放在仓库 `docs/tech-debt.md` 或项目管理工具），按 P0→P1→P2 排期消项，每条关联 PR。

---

## 2. 代码质量把控体系（Quality Control）

### 2.1 编码规范（三栈统一要求）

**Go（后端）**
- 遵循 [uber-go/guide](https://github.com/uber-go/guide)；`gofmt` / `goimports` 统一格式。
- 静态检查：`go vet` + `staticcheck` + `golangci-lint`（CI 零容忍）。
- 错误处理：`error` 必须包装上下文（`fmt.Errorf("list materials: %w", err)`）；handler 层禁止 `panic`。
- 日志：用 `log/slog` 结构化日志，禁止 `println`；敏感字段脱敏。
- 关键数据路径（权限/计费）优先用 `sqlc` 生成类型安全 SQL，减少 GORM 魔法。
- Context 必须透传（超时 / 取消 / trace）。

**TypeScript / React（前端）**
- `strict: true`，禁止 `any`；用 `zod` 校验后端响应。
- `eslint`(typescript + react-hooks) + `prettier`，提交前 husky 自动跑。
- 服务端状态用 `react-query`；SSE 用 `fetch` + `ReadableStream`（可带 `Authorization` Header），**不要用 `EventSource`** 以避免 token 进 URL（呼应 R4）。
- 错误边界 `ErrorBoundary` 兜底；组件就近 co-locate。

**Python（Agent）**
- `ruff`（lint+format）+ `mypy --strict` 类型零容忍。
- 所有 I/O 边界用 `pydantic v2` 模型；`asyncio` 纪律：不在同步函数里阻塞。
- 外部 HTTP 用 `httpx`，强制 timeout + 有限重试（呼应 R6）。
- 结构化日志（避免 `print`）。

### 2.2 CI 质量门禁（PR 合入前必须全绿）

| 门禁 | 工具 | 不通过处理 |
|------|------|-----------|
| 格式 / Lint | golangci-lint / eslint+prettier / ruff | 阻断 |
| 构建 | go build / tsc / pytest 收集 | 阻断 |
| 单测 + 覆盖率 | go test -cover / vitest / pytest | 整体 ≥ 70% 阻断；**权限/安全路径 ≥ 90%** 阻断 |
| 安全扫描 | gosec / bandit / semgrep | High/Critical 阻断 |
| 契约测试 | 后端↔Agent API 契约 | 阻断 |
| 架构守护 | 依赖方向 test（handler→service→repo，禁止反向） | 阻断 |

> 覆盖率门槛分两档：普通逻辑 70%，**权限与计费等高危路径 90%**——这是本项目「权限即生命线」的硬要求。

### 2.3 代码评审清单（PR 模板，4-eyes 原则）

每个 PR 必须勾选以下内容，且至少 1 名资深开发 approval 才能合入：

```
## 自评清单
- [ ] 安全：权限过滤是否仅在 repository 层？新接口是否 RBAC 校验？是否带越权测试？
- [ ] 错误：所有外部调用（DB/Agent/Redis/对象存储）都有 timeout/retry？无裸 panic？
- [ ] 并发：共享状态（成员关系 / shared 翻转）是否有锁或事务保护？
- [ ] 可观测：关键路径有 trace/metric/log？耗时操作有超时预算？
- [ ] 测试：新增逻辑有单测？边界 / 异常分支覆盖？
- [ ] 性能：是否有 N+1？向量检索是否限制 top-k？
- [ ] 文档：API / 表结构变更是否同步 docs/？
```

### 2.4 测试策略（分三层）

1. **单测（快）**：repository 层权限谓词（断言 `shared=false` 的 teacher 资料被排除）、计费限流逻辑。
2. **集成测试（中）**：完整链路「上传资料→解析→RAG 答疑→断言返回片段来自可见 team」「学生问答绝不引用不可见资料」。
3. **契约测试（稳）**：后端与 Agent 的 A2A/HTTP 接口 schema 双向校验，避免一侧改了另一侧不知道。

---

## 3. 团队能力分级与成长路径（Capability Ladder）

| 等级 | 名称 | 行为标准 | 适合负责 |
|------|------|----------|----------|
| **L1** | 实现者 | 能在框架内写 CRUD / 组件；理解 Git 流程；写基础单测 | 资料 CRUD、前端页面、简单 API |
| **L2** | 可靠者 | 写带测试的模块；懂并发/错误处理/事务；能独立排查 bug | repository 层、解析流水线、前端状态 |
| **L3** | 设计者 | 能设计服务/API；性能与安全意识自觉；评审他人代码 | 权限模型、RAG 检索、Agent 编排 |
| **L4** | 技术领导 | 跨服务架构决策； mentoring；把控技术债与质量文化 | 系统架构、技术选型、风险治理 |

**成长机制（低成本的团队提升杠杆）**
- **师徒 / 结对**：L3+ 带 L1/L2 做核心模块（权限、RAG），边做边讲「为什么这样隔离」。
- **评审即训练场**：每个 PR 至少留 1 条「可学习点」comment（不是挑错，是传授）。
- **双周技术分享**：RAG/Agent 工程、Go 并发模型、向量检索原理、可观测性实践。
- **无责复盘（blameless postmortem）**：线上问题复盘聚焦系统而非个人，沉淀为 §1 技术债。

---

## 4. 工程化基建（Engineering Foundation）

- **统一仓库结构**：`frontend/` `backend/` `agent/` 三服务 + 共享 CI 模板（或 monorepo）。
- **脚手架**：各服务 `cookiecutter` 模板，一键生成符合 §2.1 规范的起步代码，避免「每人一套风格」。
- **一键起全栈**：`docker-compose.yml` 起 postgres+pgvector、redis、minio，再加三个服务，新人 10 分钟跑通。
- **可观测性底座**：OpenTelemetry 统一 trace/metric/log；Grafana 看板盯 RAG 延迟、Agent 各跳耗时、限流命中。
- **环境一致性**：`devcontainer` 或 `asdf` 锁定 Node/Go/Python 版本，杜绝「我本地能跑」。

---

## 5. 质量文化（Culture & Metrics）

- **技术债看板**：把 §1 的 R1–R10 登记为卡片，关联 PR，定期消项。
- **度量（月度看趋势）**：
  - 评审轮次（目标下降，说明自审质量提升）
  - 缺陷逃逸率（生产 bug / 总缺陷）
  - 测试覆盖率趋势（尤其高危路径）
  - MTTR（平均修复时间）
- **红线意识**：权限/计费相关改动**必须带测试**，否则资深开发有权打回。

---

## 6. 90 天落地路线图

| 阶段 | 周期 | 重点 | 产出 |
|------|------|------|------|
| **阶段一 · 止血** | 第 1–4 周 | CI 质量门禁（§2.2）+ PR 评审清单（§2.3）+ R1/R2/R3 闭环 | 合入即过门禁；权限测试红绿驱动 |
| **阶段二 · 能力** | 第 5–8 周 | 结对 / 师徒（§3）+ 双周分享 + R4–R8 消项 | L1→L2 明显成长；分享会运转 |
| **阶段三 · 体系** | 第 9–12 周 | 可观测性（R9）+ 技术债看板 + 度量（§5） | 全链路 trace；质量度量看板 |

---

## 附：与既有文档的关系

- `README.md` / `docs/system-design.md`（v0.3）：系统怎么建 → 本手册管「建的时候怎么不出错、团队怎么长大」。
- `docs/prd.md`：做什么、为谁做 → 本手册管「做的人够不够稳」。
- 建议把本手册纳入新人 onboarding 必读，与三份文档配套。
