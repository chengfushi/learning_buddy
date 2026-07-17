# Backend 服务（Go + Gin）

> 智能学伴系统后端——鉴权 / Team & RBAC / 资料 CRUD / 权限过滤与向量检索 / 学习记录 / Agent 网关。不负责模型推理。

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-v1.10-0095D5)](https://gin-gonic.com/)
[![GORM](https://img.shields.io/badge/GORM-v1.25-00ADD8)](https://gorm.io/)

---

## 架构概览 / Architecture

```
handler (Gin) → service (业务/RBAC/可见 team) → repository (仅此层拼 SQL/GORM) → PostgreSQL
                        ↘ agent client (调用 Agent 服务, SSE 转发)
```

**分层职责（铁律）：**

| 层 | 目录 | 职责 | 禁止 |
|----|------|------|------|
| Handler | `internal/handler/` | 路由注册、请求校验、响应序列化 | 不写业务逻辑与权限 SQL |
| Service | `internal/service/` | 业务编排、RBAC 判定、可见 team 计算 | 不拼权限谓词 SQL |
| Repository | `internal/repository/` | 数据访问、**权限谓词唯一拼装点** | — |
| Middleware | `internal/middleware/` | JWT 鉴权、角色校验 | 不写业务逻辑 |

> **安全铁律**：用户可见资料范围的权限谓词**只能**在 repository 层构建。Agent 与前端永远不直接拼权限 SQL。违反视为严重缺陷。

---

## 技术栈 / Tech Stack

| 组件 | 版本 / 库 | 说明 |
|------|-----------|------|
| 语言 | Go 1.25 | — |
| Web 框架 | Gin v1.10 | 路由、中间件、参数绑定 |
| ORM | GORM v1.25 + PostgreSQL 驱动 | 数据库访问 |
| 鉴权 | golang-jwt/jwt v5 | JWT access + refresh token |
| 密码 | golang.org/x/crypto | bcrypt 哈希 |
| 配置 | godotenv | 环境变量（.env） |
| 测试 | testify | 断言库 |

---

## 快速开始 / Quick Start

### 前置依赖

- Go 1.22+（推荐 1.25）
- PostgreSQL 16（启用 `pgvector` 扩展）
- Redis 7
- Agent 服务运行中（默认 `http://localhost:8000`）

### 安装与运行

```bash
cd backend

# 1. 配置环境变量
cp .env.example .env
# 编辑 .env：填入 JWT_SECRET、DB_DSN 等

# 2. 安装依赖
go mod tidy

# 3. 初始化或升级数据库
DB_DSN="$DB_DSN" make -C .. migrate

# 4. 用管理员 DSN 创建 Parser 最小权限账号（密码不入库）
DB_DSN="$DB_DSN" PARSER_DB_PASSWORD='replace-me' make -C .. provision-parser
# 当前顺序：0001 → 0002 → 0003 → 0004 → 0005 → 0006 → 0007

# 5. 启动
go run main.go
# 默认监听 :8080，启动时自动断言 embedding 维度一致性（R1）
```

---

## 目录结构 / Project Structure

```
backend/
├── main.go                    # 入口：初始化 DB → 注入依赖 → 注册路由 → 启动服务
├── go.mod / go.sum            # 依赖管理
├── Dockerfile                 # 容器构建
├── .env.example               # 环境变量模板
├── .golangci.yml              # golangci-lint 配置
├── migrations/                # 数据库迁移脚本
│   ├── 0001_init.sql          #   建表（users / teams / materials / material_chunks ...）
│   ├── 0002_seed.sql          #   初始种子数据
│   ├── 0003_fix_sequences.sql #   序列修复
│   ├── 0004_embedding_1024.sql #   embedding 列维度升级为 1024
│   ├── 0005_material_chunks_idempotency.sql # chunks 幂等索引
│   ├── 0006_material_parse_generation.sql # 解析代次隔离陈旧 worker
│   └── 0007_exercise_ownership.sql # 测评题目所有者隔离
└── internal/
    ├── handler/               # 路由注册与请求校验
    │   ├── router.go          #   全部路由注册（公开 + 鉴权）
    │   ├── auth.go            #   注册 / 登录 / 刷新
    │   ├── teams.go           #   team CRUD / 加入 / 审批 / 成员列表
    │   ├── materials.go       #   资料 CRUD（归属 team、shared 可见性、异步解析）
    │   ├── notes.go           #   笔记 CRUD（阅读器标注）
    │   ├── learning.go        #   学习记录 / 进度聚合
    │   ├── plans.go           #   学习计划（Planner Agent 代理）
    │   └── agent.go           #   AI 对话（SSE 流式代理）
    ├── service/               # 业务逻辑层
    │   ├── service.go         #   服务聚合（依赖注入）
    │   ├── auth.go            #   鉴权逻辑（JWT 签发 / 校验）
    │   ├── team.go            #   team 业务（创建 / 加入 / 审批 / 可见集合计算）
    │   ├── material.go        #   资料业务（上传 / 更新 / shared 翻转 / 持久化解析调度）
    │   ├── learning.go        #   学习记录业务
    │   ├── conversation.go    #   对话会话管理
    │   ├── agent.go           #   Agent HTTP 客户端（超时 / 重试 / SSE 转发）
    │   └── team_flow_test.go  #   team 流程集成测试
    ├── repository/            # 数据访问层（权限谓词唯一拼装点）
    │   ├── repository.go      #   可见 team 集合计算 + 可见资料 Scope（R2 核心）
    │   ├── team_repo.go       #   team / 成员关系 CRUD
    │   ├── data_repo.go       #   通用数据访问
    │   └── permission_test.go #   权限单元测试
    ├── middleware/             # 中间件
    │   └── auth.go            #   JWT 鉴权 + RBAC 角色校验
    ├── model/                 # 数据模型
    │   ├── models.go          #   GORM 模型（users / teams / materials / learning_records ...）
    │   └── types.go           #   请求/响应类型定义
    └── config/                # 配置管理
        └── config.go          #   环境变量加载 + 默认值
```

---

## 环境变量 / Environment Variables

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DB_DSN` | PostgreSQL 连接串 | `postgres://postgres:postgres@localhost:5432/learning_buddy?sslmode=disable` |
| `REDIS_ADDR` | Redis 地址 | （预留） |
| `JWT_SECRET` | JWT 签名密钥 | `dev-secret-change-me-please-32bytes+`（**生产必须修改**） |
| `AGENT_BASE_URL` | Agent 服务地址 | `http://localhost:8000` |
| `AGENT_SHARED_SECRET` | Backend→Agent 服务认证共享密钥（必填） | — |
| `PARSE_ALERT_WEBHOOK_URL` | 解析任务重试耗尽告警 Webhook（可选，5 秒超时） | — |
| `MINIO_ENDPOINT` | 对象存储端点 | — |
| `MINIO_BUCKET` | 存储桶名称 | `materials` |
| `MINIO_ACCESS_KEY` | 对象存储访问密钥 | — |
| `MINIO_SECRET_KEY` | 对象存储密钥 | — |
| `EMBEDDING_DIM` | 向量维度（启动断言） | `1024` |
| `ADDR` | HTTP 监听地址 | `:8080` |
| `UPLOAD_DIR` | 本地文件上传目录 | `./data/uploads` |

---

## API 路由 / API Routes

> 完整 API 设计见项目根目录 [`docs/system-design.md`](../docs/system-design.md) §6.5。

### 公开接口

| Method | Path | 说明 |
|--------|------|------|
| `POST` | `/api/auth/register` | 注册（默认 student，自动建私人 team） |
| `POST` | `/api/auth/login` | 登录，返回 JWT（access + refresh） |
| `POST` | `/api/auth/refresh` | 刷新 access token |

### 鉴权接口（需 Bearer Token）

| Method | Path | 说明 | RBAC |
|--------|------|------|------|
| `GET` | `/api/me` | 当前用户信息 | 所有角色 |
| `GET` | `/api/teams` | 可见 team 列表 | 所有角色 |
| `POST` | `/api/teams` | 创建学习小组（返回 `join_code`） | `teacher` |
| `POST` | `/api/teams/:id/join` | 申请加入 team（→ `pending`） | `student` |
| `POST` | `/api/teams/join` | 凭 `join_code` 加入 | `student` |
| `POST` | `/api/teams/:id/members/:uid/approve` | 审批成员 | `teacher`(owner) |
| `GET` | `/api/teams/:id/members` | 成员与待审批列表 | `teacher`(owner) |
| `GET` | `/api/teams/:id/materials` | team 内可见资料（成员审批 + `shared`） | 所有角色 |
| `GET` | `/api/materials` | 资料列表（按可见 team 过滤） | 所有角色 |
| `POST` | `/api/materials` | 上传资料（异步触发解析） | 所有角色 |
| `GET` | `/api/materials/:id` | 资料详情（含 `parse_status`） | 所有角色 |
| `PUT` | `/api/materials/:id` | 更新资料 / 切 `shared` | 所有者 |
| `POST` | `/api/materials/:id/retry` | 重试失败的解析任务 | 所有者 |
| `DELETE` | `/api/materials/:id` | 删除资料（级联删 chunks） | 所有者 |
| `GET` | `/api/materials/:id/notes` | 自己的笔记列表（需资料可见） | 所有角色 |
| `POST` | `/api/materials/:id/notes` | 创建笔记（需资料可见） | 所有角色 |
| `PUT` | `/api/notes/:id` | 更新笔记 | 所有者 |
| `DELETE` | `/api/notes/:id` | 删除笔记 | 所有者 |
| `POST` | `/api/learning/records` | 创建学习记录 | 所有角色 |
| `GET` | `/api/learning/records` | 学习记录列表 | 所有角色 |
| `GET` | `/api/learning/progress` | 学习进度聚合 | 所有角色 |
| `GET` | `/api/agent/sessions` | 会话列表 | 所有角色 |
| `GET` | `/api/agent/sessions/:id` | 会话详情 | 所有角色 |
| `POST` | `/api/agent/chat` | AI 对话（**SSE 流式**） | 所有角色 |
| `POST` | `/api/agent/plan` | 生成学习计划 | 所有角色 |
| `POST` | `/api/agent/quiz` | 生成测评题目 | 所有角色 |
| `POST` | `/api/agent/quiz/:id/answer` | 提交答案并批改 | 所有角色 |

---

## 权限模型 / RBAC & Team Visibility

三类角色，权限以「团队 / 知识库」为边界：

| 角色 | 团队行为 | 资料权限 |
|------|----------|----------|
| `student` | 自动拥有私人 team；凭 `join_code` 加入老师 team（需审批） | 私人资料仅自己可见；可访问已加入 teacher team 中 `shared=true` 资料 + 公共库 |
| `teacher` | 创建学习小组 team（生成 `join_code`）；审批学生加入 | 仅老师能在自己 team 上传资料；逐份设置 `shared` 控制可见性 |
| `super_admin` | 维护公共库（`type='public'`，不写 `team_members`） | 上传资料全平台可见 |

**可见资料谓词**（仅 repository 层构建）：

```sql
team_id IN (可见集合) AND (teams.type <> 'teacher' OR materials.shared = true)
```

---

## 常用命令 / Commands

```bash
# 格式化
gofmt -w .

# 静态检查
golangci-lint run

# 运行测试
go test ./...

# 运行单个包的测试
go test ./internal/repository/ -v

# 启动（开发模式）
go run main.go
```

---

## 设计要点 / Design Notes

1. **启动时维度断言（R1）**：`main.go` 启动时自动查询 `material_chunks.embedding` 列类型，与配置的 `EMBEDDING_DIM` 比对，不一致则拒绝启动，防止 RAG 静默返回垃圾。
2. **权限集中（R2）**：`repository.VisibleTeamIDs()` + `VisibleMaterialsScope()` 是用户可见资料范围的唯一真源。详情、team 列表与笔记入口均复用该 scope；不可见和不存在统一返回 404，避免 ID 枚举。
3. **分层清晰**：Handler 只管校验+序列化，Service 编排业务，Repository 拼 SQL——禁止反向依赖。
4. **Agent 边界**：后端 repository 完成资料权限过滤与 pgvector top-k，仅向 Agent 下发已授权 chunks。Agent Parser 使用最小写权限更新正文与 chunks，但不能访问用户、成员或认证数据。
5. **SSE 鉴权**：前端使用 `fetch` + `ReadableStream` 实现 SSE（可带 `Authorization` Header），避免 `EventSource` 将 token 暴露在 URL 中（R4）。
6. **历史题目升级**：`0007` 优先从会话或唯一作答用户回填题目所有者；无法安全确定所有者的旧题及作答记录会被显式失效清理，随后数据库强制 `exercises.user_id NOT NULL`。
