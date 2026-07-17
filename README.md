# 智能学伴系统（AI Learning Companion）

> 一个由多智能体驱动的 AI 学伴——让学生既能自主按节奏学习，也能随时获得讲解、规划与测评；资料以「团队知识库」组织，越用越懂你。

[![Stack](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Stack](https://img.shields.io/badge/React-18-61DAFB?logo=react)](https://react.dev/)
[![Stack](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python)](https://www.python.org/)
[![Stack](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql)](https://www.postgresql.org/)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

---

## 技术栈 / Architecture

```
用户 → 前端(React 18) → 后端(Go/Gin, RBAC+Team+pgvector 检索) → PostgreSQL + Redis
                              │
                              └─▶ Agent(Python/FastAPI，消费已授权 chunks)
```

| 层 | 技术 | 说明 |
|----|------|------|
| 前端 | React 18 + TypeScript 5 + Vite 5 + Zod | SPA，React Query 管理服务端状态，SSE 流式对话 |
| 后端 | Go 1.25 + Gin + GORM + JWT | RESTful API，RBAC + Team 权限模型，资料权限过滤与 pgvector 检索，Agent 请求代理 |
| Agent | Python 3.11+ + FastAPI + psycopg2 + pgvector | 资料解析与多智能体生成（答疑 / 规划 / 测评），仅消费 Backend 已授权 chunks |
| 关系库 | PostgreSQL 16 + pgvector | 业务数据 + 向量存储，IVFFlat 索引 |
| 缓存 | Redis 7 | 会话上下文、热点缓存、限流 |
| 文件 | MinIO / S3 | 学习资料文件存储 |

> 详细架构、模块设计、数据模型参见 [`docs/system-design.md`](docs/system-design.md)；产品背景与路线图参见 [`docs/prd.md`](docs/prd.md)；工程规范参见 [`docs/engineering-standards.md`](docs/engineering-standards.md)。

---

## 功能特性 / Features

- 📚 **资料库 / 团队知识库**：以「团队 = 知识库」组织资料——老师建学习小组并控制资料可见性（`shared`），学生有私有 team，超级管理员维护全平台公共库。上传即自动解析入库（Parser Agent）。
- 🤖 **AI 答疑**：选中内容随时提问，基于「可见 team 集合」RAG 检索，流式回答并附资料引用来源（标明 team / 资料 / 章节）。
- 📝 **阅读器 + 笔记**：资料阅读、笔记标注、一键提问。
- 🗺️ **学习计划**：Planner Agent 按目标与期限生成个性化学习路径（落 `study_plans`）。
- ✅ **智能测评**：Evaluator Agent 自动出题、批改并给出薄弱点分析（落 `exercises` / `quiz_attempts`）。
- 📊 **进度看板**：学习时长、完成度、正确率趋势可视化。
- 💬 **对话记忆**：多轮上下文 + 历史会话回溯，长期画像记忆（Memory Agent）。
- 🔐 **三级权限**：学生（私有 team + 加入老师 team）、老师（创建学习小组 + 审批成员 + 控制资料可见性）、超级管理员（维护公共库）。

---

## 快速开始 / Quick Start

RAG v2 的迁移、影子评测、灰度切换与回滚步骤见 [生产运行手册](docs/rag-v2-production.md)。

### 前置依赖 / Prerequisites

| 依赖 | 版本要求 | 说明 |
|------|----------|------|
| Node.js | 18+ | 前端开发与构建 |
| Go | 1.22+（推荐 1.25） | 后端编译运行 |
| Python | 3.11+ | Agent 服务 |
| Docker | 近期版本 | 一键启动基础设施（PostgreSQL + Redis + MinIO） |

### 1. 启动基础设施

```bash
# 仅启动 PostgreSQL + Redis + MinIO（不含三服务）
make infra
# 或
docker compose up -d db redis minio
```

### 2. 安装 Git 钩子（一次性）

```bash
git config core.hooksPath .githooks
```

### 3. 启动后端（Go / Gin）

```bash
cd backend
cp .env.example .env   # 编辑 .env 填入真实密钥
go mod tidy
go run main.go
# 默认监听 :8080
```

### 4. 启动 Agent 服务（Python）

```bash
cd agent
cp .env.example .env   # 编辑 .env 填入 LLM API Key
pip install -r requirements.txt
python main.py
# 默认监听 :8000
```

### 5. 启动前端（React）

```bash
cd frontend
cp .env.example .env
npm install
npm run dev
# 默认 http://localhost:5173
```

### 一键全栈启动（Docker Compose）

```bash
docker compose up       # 启动所有服务
```

---

## 目录结构 / Project Structure

```
learning_buddy/
├── frontend/                # React 前端（学习 + AI 辅助两大模式）
│   ├── src/
│   │   ├── pages/           # Login / Teams / Library / Reader / Learning / Companion
│   │   ├── api.ts           # 后端 API 封装
│   │   ├── auth.tsx         # 鉴权上下文
│   │   └── main.tsx         # 入口
│   ├── vite.config.ts
│   └── package.json
├── backend/                 # Go + Gin 后端（鉴权 / Team / 资料 CRUD / Agent 网关）
│   ├── internal/
│   │   ├── handler/         # 路由注册与请求校验（auth / teams / materials / notes / agent / learning）
│   │   ├── service/         # 业务逻辑层（auth / team / material / conversation / agent / learning）
│   │   ├── repository/      # 数据访问层（权限谓词在此强制）
│   │   ├── middleware/      # JWT 鉴权 + RBAC 校验中间件
│   │   ├── model/           # 数据模型与类型定义
│   │   └── config/          # 环境变量配置
│   ├── migrations/          # 数据库迁移脚本（DDL + 种子数据）
│   ├── main.go
│   └── go.mod
├── agent/                   # Python Agent 服务（RAG + 多智能体）
│   ├── main.py              # FastAPI 入口
│   ├── db.py                # 数据库连接（Parser 最小读写凭证）
│   ├── embed.py             # Embedding 生成（DashScope 1024 维）
│   ├── llm.py               # LLM 调用（DeepSeek）
│   ├── rag.py               # 解析与基于已授权 chunks 的生成编排
│   ├── schemas.py           # Pydantic 模型
│   ├── tests/               # 单测与集成测试
│   └── requirements.txt
├── docs/                    # 项目文档
│   ├── prd.md               # 产品需求文档
│   ├── system-design.md     # 系统设计文档
│   ├── database.md          # 数据库文档
│   └── engineering-standards.md  # 工程规范与代码质量手册
├── tests/e2e/               # 端到端验收脚本
│   ├── e2e.sh               # P0 主流程
│   └── r2.sh                # R2 权限隔离
├── docker-compose.yml       # 本地基础设施 + 三服务
├── Makefile                 # 常用命令（infra / dev / format / lint / migrate）
└── .githooks/pre-commit     # 提交前自动格式化与检查
```

---

## 环境变量 / Environment Variables

各子服务均提供 `.env.example`（复制为 `.env` 后填入真实值）。

### backend/.env

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DSN` | PostgreSQL 连接串 | `postgres://learning:learning@localhost:5432/learning_buddy?sslmode=disable` |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `JWT_SECRET` | JWT 签名密钥 | —（**必须修改**） |
| `AGENT_BASE_URL` | Agent 服务地址 | `http://localhost:8000` |
| `AGENT_SHARED_SECRET` | Backend→Agent 服务认证共享密钥（必填，两端一致） | — |
| `PARSE_ALERT_WEBHOOK_URL` | 解析任务重试耗尽告警 Webhook（可选） | — |
| `MINIO_ENDPOINT` | 对象存储端点 | `localhost:9000` |
| `MINIO_BUCKET` | 存储桶名称 | `materials` |
| `MINIO_ACCESS_KEY` | 对象存储访问密钥 | `minioadmin` |
| `MINIO_SECRET_KEY` | 对象存储密钥 | `minioadmin` |
| `EMBEDDING_DIM` | 向量维度（全库统一） | `1024` |
| `ADDR` | 监听地址 | `:8080` |

### agent/.env

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `LLM_API_KEY` | LLM API 密钥（DeepSeek） | —（**必须填写**） |
| `LLM_BASE_URL` | LLM API 端点 | `https://api.deepseek.com/v1` |
| `LLM_MODEL` | 模型名称 | `deepseek-chat` |
| `PG_DSN` | Agent Parser 数据库连接串（生产使用最小读写权限） | 本地 Compose 使用 `learning` 开发用户 |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `EMBEDDING_PROVIDER` | Embedding 提供商 | `openai`（兼容模式） |
| `EMBEDDING_API_KEY` | Embedding API 密钥（DashScope） | —（**必须填写**） |
| `EMBEDDING_BASE_URL` | Embedding API 端点 | DashScope 兼容端点 |
| `EMBEDDING_MODEL` | Embedding 模型 | `text-embedding-v4` |
| `EMBEDDING_DIM` | 向量维度 | `1024` |
| `AGENT_SHARED_SECRET` | Backend→Agent 服务认证共享密钥（必填，两端一致） | — |
| `RETRIEVER_TIMEOUT_S` | 答疑检索单跳预算（秒） | `0.8` |
| `PARSER_EMBEDDING_TIMEOUT_S` | 资料解析单个 chunk 的 Embedding 超时（秒） | `30` |
| `TUTOR_TIMEOUT_S` | 答疑生成单跳预算（秒） | `30` |
| `PORT` | 监听端口 | `8000` |

### frontend/.env

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `VITE_API_BASE` | 后端 API 基地址 | `http://localhost:8080` |

---

## API 概览 / API Overview

> 完整 API 设计见 [`docs/system-design.md` §6.5](docs/system-design.md)。

### 认证 / Auth

| Method | Path | 说明 | 鉴权 |
|--------|------|------|------|
| `POST` | `/api/auth/register` | 注册（默认 student，自动建私人 team） | 否 |
| `POST` | `/api/auth/login` | 登录，返回 JWT | 否 |
| `POST` | `/api/auth/refresh` | 刷新 access token（httpOnly Cookie） | 是 |

### 团队 / Teams

| Method | Path | 说明 | 鉴权 |
|--------|------|------|------|
| `GET` | `/api/teams` | 我可见的 team 列表（私人 + 已加入 + 公共） | 是 |
| `POST` | `/api/teams` | 老师创建学习小组（返回 `join_code`） | `teacher` |
| `POST` | `/api/teams/:id/join` | 学生凭 ID 申请加入（→ `pending`） | `student` |
| `POST` | `/api/teams/join` | 学生凭 `join_code` 申请加入 | `student` |
| `POST` | `/api/teams/:id/members/:uid/approve` | 老师审批成员加入 | `teacher`(owner) |
| `GET` | `/api/teams/:id/members` | 成员与待审批列表 | `teacher`(owner) |

### 资料 / Materials

| Method | Path | 说明 | 鉴权 |
|--------|------|------|------|
| `GET` | `/api/materials` | 资料列表（按可见 team 过滤） | 是 |
| `POST` | `/api/materials` | 上传资料（异步触发解析） | 是 |
| `GET` | `/api/materials/:id` | 资料详情（含 `parse_status`） | 是 |
| `PUT` | `/api/materials/:id` | 更新资料 / 切换 `shared` 可见性 | 是 |
| `DELETE` | `/api/materials/:id` | 删除资料（级联删 chunks） | 是 |

### Agent / AI

| Method | Path | 说明 | 鉴权 |
|--------|------|------|------|
| `GET` | `/api/agent/sessions` | 我的会话列表 | 是 |
| `GET` | `/api/agent/sessions/:id` | 会话详情（含历史消息） | 是 |
| `POST` | `/api/agent/chat` | AI 对话（Backend 检索已授权 chunks，Agent **SSE 流式**生成） | 是 |
| `POST` | `/api/agent/plan` | 生成学习计划（落 `study_plans`） | 是 |
| `POST` | `/api/agent/quiz` | 生成测评题目（落 `exercises`） | 是 |
| `POST` | `/api/agent/quiz/:id/answer` | 提交测评答案并批改 | 是 |

### 学习记录 / Learning

| Method | Path | 说明 | 鉴权 |
|--------|------|------|------|
| `POST` | `/api/learning/records` | 创建学习记录 | 是 |
| `GET` | `/api/learning/records` | 我的学习记录列表 | 是 |
| `GET` | `/api/learning/progress` | 学习进度聚合 | 是 |

---

## 角色与权限 / Roles & RBAC

系统以「**团队 = 知识库**」组织资料与权限，三类角色：

| 角色 | 团队行为 | 资料权限 |
|------|----------|----------|
| **学生** `student` | 自动拥有私人 team（注册时创建）；凭 `join_code` 加入老师 team（需审批） | 私人资料仅自己可见；可访问已加入 teacher team 中 `shared=true` 的资料 + 公共库 |
| **老师** `teacher` | 创建学习小组 team，生成 `join_code`；审批学生加入 | 仅老师能在自己的 team 上传资料；逐份设置 `shared` 控制对学生可见 |
| **超级管理员** `super_admin` | 维护公共库（`type='public'`） | 上传的资料全平台可见 |

> **安全铁律**：权限谓词与 pgvector top-k 只在后端 repository 层构建；Agent 不提供 `/retrieve`，只消费已授权 chunks。Parser 凭证不得访问用户、成员或认证数据。

---

## 数据库 / Database

核心数据模型（11 张表）：

```
users ──< team_members >── teams ──< materials ──< material_chunks (vector)
users ──< learning_records ──> materials
users ──< agent_sessions ──< agent_messages
users ──< exercises ──< quiz_attempts; exercises ──> materials
users ──< study_plans
users ──< user_profiles (1:1)
users ──< token_usage
```

> 完整表结构、索引、缓存策略见 [`docs/database.md`](docs/database.md)。

### Redis 缓存键

| Key 模式 | 内容 | TTL |
|----------|------|-----|
| `session:{session_id}` | 对话上下文 | 30 min 闲置 |
| `team:visible:{user_id}` | 用户可见 team 集合 | 5 min |
| `cache:material:{id}` | 热点资料 | 10 min |
| `ratelimit:agent:{user_id}` | 对话限流 | 1 min |
| `user:profile:{user_id}` | 用户画像快照 | 5 min |

---

## 常用命令 / Commands

```bash
# 仅启动基础设施
make infra

# 一键全栈启动
make dev

# 格式化（三栈）
make format

# 静态检查（三栈）
make lint

# 数据库迁移（按文件名顺序幂等执行）
make migrate
```

---

## 贡献指南 / Contributing

1. **分支策略**：从 `main` 创建特性分支（`feat/xxx` 或 `fix/xxx`）。
2. **安装 Git 钩子**：`git config core.hooksPath .githooks`（提交前自动格式化与静态检查）。
3. **提交规范**：使用 [Conventional Commits](https://www.conventionalcommits.org/)，scope 标注服务：
   ```
   feat(backend): 资料 repository 集中化可见 team 谓词
   fix(backend): 检索超时降级为空 chunks
   docs: 补充数据库文档
   chore: 升级依赖
   ```
4. **质量门禁**：CI 必须全绿（golangci-lint / eslint+prettier / ruff / 单测覆盖率 ≥ 70%，权限/安全路径 ≥ 90%）。
5. **权限相关改动必须带测试**——含越权反向用例。
6. **文档同步**：API / 表结构变更同步更新 `docs/`。
7. **Code Review**：4-eyes 原则，至少 1 名资深开发 approval（评审清单见 [`docs/engineering-standards.md` §2.3](docs/engineering-standards.md)）。

---

## 技术债 / Tech Debt

已知技术风险按优先级登记在 [`docs/engineering-standards.md` §1](docs/engineering-standards.md)，核心 P0 项：

- **R1** Embedding 维度一致性——启动时断言，防止静默检索垃圾
- **R2** RAG 权限谓词不被绕过——只在后端 repository 层构建
- **R3** 解析队列可靠性——超时 + 幂等 + 死信 + 告警

---

## License

MIT
