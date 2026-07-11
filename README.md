# 智能学伴系统（AI Learning Companion）

> 基于 AI Agent 的智能学习陪伴系统，帮助学生更好地进行「自主学习」与「AI 辅助学习」。

智能学伴系统由三大核心模块组成，关注点分离、独立部署、独立演进：

- **前端（React）** —— 与用户直接交互，支持「自主学习」与「AI 辅助学习」两种模式。
- **后端（Go / Gin）** —— 权限管理（RBAC + 团队）、学习资料增删改查、为 Agent 提供数据查询与请求代理。
- **Agent 服务（Python / Google ADK + A2A）** —— 多智能体协作（解析 / 检索 / 答疑 / 规划 / 测评），基于**团队知识库** RAG 提供个性化辅助。

辅助存储：**PostgreSQL + pgvector**（关系 + 向量）、**Redis**（会话缓存 / 热点缓存 / 限流）、**对象存储 MinIO/S3**（学习资料文件）。

---

## 角色与团队知识库

系统以「**团队 = 知识库**」组织资料与权限，三类角色：

- **学生**：拥有系统自动生成的**私人 team**（资料仅自己可见），可加入老师的 team 访问公开资料，默认可访问公共库。
- **老师**：可创建**学习小组 team**，仅老师能上传资料，并逐份设置是否对学生可见（`shared`）。
- **超级管理员**：维护系统级**公共库**，资料全平台可见。

> 所有资料上传后由 Agent 结构化解析，落入对应 team 的向量/结构化存储；RAG 检索严格限定在用户有权访问的 team 集合内。

---

## 功能特性

- 📚 **资料库 / 团队知识库**：老师建学习小组并控制资料可见性，学生有私有 team，超级管理员维护全平台公共库；资料由 Agent 结构化解析入库。
- 🤖 **AI 答疑**：选中内容随时提问，流式回答并附资料引用来源（标明 team / 资料）。
- 🗺️ **学习计划**：根据目标与期限生成个性化学习路径。
- ✅ **智能测评**：自动出题、批改并给出薄弱点分析。
- 📊 **进度看板**：学习时长、完成度、正确率趋势可视化。
- 💬 **对话记忆**：多轮上下文 + 历史会话回溯，越用越懂你。

---

## 技术架构

| 层 | 技术 |
|----|------|
| 前端 | React 18 + TypeScript + Vite |
| 后端 | Go 1.25 + Gin + GORM |
| Agent | Python 3.13 + Google ADK + A2A Protocol |
| 关系库 | PostgreSQL 17 |
| 向量库 | pgvector |
| 缓存 | Redis 7 |

```
用户 → 前端(React) → 后端(Go/Gin, RBAC+Team) → PostgreSQL + Redis
                             │
                             └─▶ Agent(Python/ADK/A2A) → 按 team 集合检索 pgvector
```

> 更详细的架构、模块设计、数据模型与 API 见 [`docs/system-design.md`](docs/system-design.md)；
> 产品背景、用户痛点、功能与路线图见 [`docs/prd.md`](docs/prd.md)。

---

## 目录结构

```
learning_buddy/
├── frontend/          # React 前端（自主学习 + AI 辅助学习）
├── backend/           # Go + Gin 后端（鉴权 / Team / 资料 CRUD / Agent 网关）
├── agent/             # Python Agent 服务（Google ADK + A2A 多智能体）
├── docs/
│   ├── system-design.md   # 系统设计文档
│   └── prd.md             # 产品需求文档（PRD）
├── docker-compose.yml # 本地基础设施（postgres + redis）
└── README.md
```

---

## 快速开始

### 前置依赖

- Node.js 18+
- Go 1.22+
- Python 3.11+
- PostgreSQL 16（启用 `pgvector` 扩展）
- Redis 7
- Docker（可选，用于一键起基础设施）

### 1. 启动基础设施

```bash
docker compose up -d db redis
```

### 2. 启动后端（Go / Gin）

```bash
cd backend
go mod tidy
go run main.go
# 默认监听 :8080
```

### 3. 启动 Agent 服务（Python）

```bash
cd agent
pip install -r requirements.txt
python main.py
# 默认监听 :8000（A2A 端口按配置）
```

### 4. 启动前端（React）

```bash
cd frontend
npm install
npm run dev
# 默认 http://localhost:5173
```

---

## 环境变量

各子服务均提供 `.env.example`，关键变量：

**backend/.env**
```
DB_DSN=postgres://user:pass@localhost:5432/learning_buddy?sslmode=disable
REDIS_ADDR=localhost:6379
JWT_SECRET=change-me
AGENT_BASE_URL=http://localhost:8000
MINIO_ENDPOINT=localhost:9000
MINIO_BUCKET=materials
```

**agent/.env**
```
LLM_API_KEY=your-key
PG_DSN=postgres://user:pass@localhost:5432/learning_buddy
REDIS_ADDR=localhost:6379
EMBEDDING_DIM=1536
```

**frontend/.env**
```
VITE_API_BASE=http://localhost:8080
```

---

## 贡献指南

1. Fork 并创建特性分支（`feat/xxx`）。
2. 提交前确保各服务可本地启动、核心链路联通。
3. 文档变更同步更新 `docs/`。
4. 发起 Pull Request，描述变更与验证方式。

---

## License

MIT
