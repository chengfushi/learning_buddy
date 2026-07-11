# 智能学伴系统 · 系统设计文档

> 版本：v0.3 · 状态：设计稿（修订） · 最后更新：2026-07-11
> 变更：v0.3 修复需求审核问题——RAG 加 `shared` 强制过滤、Agent 只读凭证、班级码+审批态、文件/解析状态机、embedding 维度配置、补四张缺表（exercises/quiz_attempts/study_plans/user_profiles/token_usage）、订阅额度接限流、K12 内容安全、路线图重排

---

## 1. 文档信息

| 项 | 内容 |
|----|------|
| 项目名称 | 智能学伴系统（AI Learning Companion） |
| 文档类型 | 系统架构 / 详细设计 |
| 适用读者 | 研发、架构、技术负责人、产品 |
| 技术栈 | React · Go/Gin · Python/Google ADK/A2A · PostgreSQL + pgvector · Redis |

---

## 2. 系统概述

### 2.1 背景与目标

智能学伴系统利用大模型与多智能体（Multi-Agent）能力，为学生提供「自主学习」与「AI 辅助学习」两套闭环体验：

- **自主学习**：学生按节奏浏览、练习资料，系统记录轨迹并给出反馈。
- **AI 辅助学习**：学生向 Agent 提问、获取讲解、生成计划与测评，Agent 基于**团队知识库**（RAG）给出个性化、可溯源的辅助。

系统以「**团队 = 知识库**」组织资料与权限：老师建学习小组、学生有私有 team、超级管理员维护公共库，所有资料经 Agent 结构化解析后落入对应 team 的向量/结构化存储。

### 2.2 设计原则

1. **关注点分离**：前端（交互）/ 后端（业务/数据）/ Agent（智能）三层独立部署。
2. **团队即知识库**：资料、向量、检索权限都以 team 为边界，隔离在检索层天然生效。
3. **RAG 优先**：Agent 回答优先基于受控资料库，降低幻觉、保证可追溯。
4. **有状态但可控**：对话上下文用 Redis 缓存，长期记忆落库。
5. **最小权限**：RBAC + team 可见性 + 行级隔离，学生只能访问授权资料与自己的数据。

---

## 3. 总体架构

```
┌─────────────────────────────────────────────────────────────┐
│                        用户 / 浏览器                          │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTPS / REST + WebSocket(SSE)
┌───────────────────────────▼─────────────────────────────────┐
│                  前端层  (React + TypeScript)                  │
│  资料库/团队 │ AI 辅助学习 │ 学习中心 │ 个人中心               │
└───────────────────────────┬─────────────────────────────────┘
                            │ REST API (JWT 鉴权)
┌───────────────────────────▼─────────────────────────────────┐
│              后端层  (Go + Gin)  —— 业务 & 数据网关            │
│  认证/授权 │ Team&RBAC │ 资料 CRUD │ 学习记录 │ Agent 网关      │
│  · 计算「用户可见 team 集合」→ 下发给 Agent 做检索过滤         │
└──────┬──────────────────────┬───────────────────┬───────────┘
       │                      │                   │
       ▼                      ▼                   ▼
┌──────────────┐     ┌────────────────┐   ┌──────────────────────┐
│ PostgreSQL   │     │   Redis 7      │   │  Agent 层 (Python)    │
│ + pgvector   │     │ 会话/热点/限流  │   │  Google ADK + A2A     │
│ teams/资料/向量│     │               │   │  Orchestrator+Retriever│
└──────────────┘     └────────────────┘   │  +Tutor+Planner+Evaluator│
                                          └──────────┬───────────┘
                                                     │ A2A (HTTP/JSON)
                                          ┌──────────▼───────────┐
                                          │ Retriever 按「可见 team  │
                                          │ 集合 + shared 谓词」检索 │
                                          │ pgvector（只读凭证）    │
                                          └──────────────────────┘
```

### 3.1 分层职责

| 层 | 职责 | 不负责 |
|----|------|--------|
| 前端 | 交互、状态、渲染、调用后端 API | 业务逻辑、数据存储、模型推理 |
| 后端 | 鉴权、Team/RBAC、资料 CRUD、计算可见 team 集合、Agent 请求代理 | 模型推理、向量检索实现 |
| Agent | 意图理解、资料解析、RAG、多 Agent 协作、生成回答 | 用户体系、资料权限判定（由后端把关） |
| 数据 | 持久化、向量检索、缓存 | 业务逻辑 |

---

## 4. 技术栈总览

| 层 | 技术选型 | 说明 |
|----|----------|------|
| 前端 | React 18 + TypeScript + Vite + Zustand + React Router | SPA，组件化 |
| 后端 | Go 1.22 + Gin + GORM + JWT | 轻量、高并发 |
| Agent | Python 3.11 + Google ADK + A2A SDK | 多智能体编排 |
| 关系库 | PostgreSQL 16 | 业务数据 |
| 向量 | pgvector 扩展 | 资料 Embedding 检索 |
| 缓存 | Redis 7 | 会话上下文、热点缓存、限流 |

---

## 5. 前端设计（React）

### 5.1 架构

- **路由**：React Router，按「资料库 / 学习中心 / AI 学伴 / 个人中心」分模块。
- **状态管理**：Zustand（全局）+ React Query（服务端缓存）。
- **UI**：组件库 + 设计 token 统一管理主题。
- **实时**：AI 对话走 SSE 流式，后端 `/api/agent/chat` 代理到 Agent。

### 5.2 自主学习模式

- 资料浏览/检索（按 team、学科、章节、标签）。
- 阅读器 + 笔记 + 标注。
- 章节练习，提交后由后端记录成绩。
- 进度看板：学习时长、完成度、正确率趋势。

### 5.3 AI 辅助学习模式

- **智能答疑**：选中文本/整页提问，Agent 基于「用户可见 team 集合」的多个知识库 RAG 回答并附引用（标明来源 team / 资料）。
- **学习规划**：Planner Agent 按目标与期限生成计划。
- **智能测评**：Evaluator Agent 生成测验并批改。
- **对话式伴学**：多轮对话、记忆上下文、可回溯历史会话。

### 5.4 关键页面

| 页面 | 说明 |
|------|------|
| 登录/注册 | 邮箱或三方登录 |
| 我的团队 | 私人 team + 已加入的老师 team + 公共库入口 |
| 资料库 | 按 team 列出资料，老师可切 `shared` 可见性 |
| 阅读器 | 内容 + 笔记 + 一键提问 |
| AI 学伴 | 对话（流式）、历史会话、引用来源 |
| 学习中心 | 计划、测评、进度可视化 |
| 管理后台 | 超级管理员：公共库管理 |

---

## 6. 后端设计（Go + Gin）

### 6.1 分层

```
handler (Gin) → service (业务/RBAC/team) → repository (GORM) → PostgreSQL
                                  ↘ agent client (调用 Agent 服务)
```

- **Middleware**：JWT 鉴权、RBAC 校验、可见 team 计算、限流、日志/追踪。
- **Agent Client**：封装对 Agent 的 HTTP 调用，含超时、重试、SSE 流式转发；请求中携带「用户可见 team 集合」。

### 6.2 权限模型（RBAC）与团队（Team）

系统存在三类角色，权限与资料归属以「团队 / 知识库」为边界：

| 角色 | 团队行为 | 资料权限 |
|------|----------|----------|
| `student`（学生） | 拥有系统自动生成的**私人 team**（注册时创建）；可凭**班级码**加入老师的 team | 仅自己可见私人资料；可访问已加入老师 team 中「对学生公开」的资料；可访问公共库 |
| `teacher`（老师） | 可创建**学习小组 team**（生成班级码 `join_code`），审批学生加入 | 仅老师能在自己的 team 上传资料；可逐份设置该资料是否对 team 内学生可见 |
| `super_admin`（超级管理员） | 管理系统级**公共库**（单一虚拟 team，`type='public'`） | 上传的资料对所有人生效，全平台可见 |

**上传权限规则（repository 层强制）**

| 操作 | 允许角色 | 说明 |
|------|----------|------|
| 上传到私人 team | 仅该 team 拥有者（学生本人） | 他人不可写 |
| 上传到 teacher team | 仅该 team 的 owner 老师 | 学生/其他老师不可写 |
| 上传到 public team | 仅 super_admin | — |
| 设置 `shared` | 仅 teacher team 的材料生效 | 学生私人材料、public 材料的 `shared` 字段被忽略/拒绝写入 |

**成员关系与审批**
- `team_members` 增加 `status`：`pending`（待审批）/ `approved`（已加入）。
- 学生凭老师提供的 `join_code` 调用 `POST /api/teams/:id/join` → 进入 `pending`；老师 `approve` 后转 `approved`，才开始可见该 team 资料。
- 公共库（`type='public'`）**不落 `team_members` 行**，在「可见 team 集合」计算中特判，避免成员表膨胀。

- 权限点示例：`material:read`、`material:write`、`team:create`、`team:manage`、`team:approve`、`user:manage`、`agent:chat`。
- 数据隔离：学习记录、对话历史按 `user_id` 行级隔离；资料按 `team_id` + `shared` 控制可见性。
- 关键规则：**每个 team 即一个知识库**；检索严格限定在「用户可见 team 集合」内，且 teacher team 仅取 `shared=true` 的材料。

### 6.3 团队与知识库（Team & Knowledge Base）

- **老师的 team（学习小组）**：老师创建（生成 `join_code`），成员为学生；仅老师可上传资料；每份资料有 `shared` 开关——开启后 team 内 `approved` 学生可见，关闭则仅老师自己可见（备课/草稿）。
- **学生的私人 team**：系统为每个学生注册时自动生成一个私有 team，学生自己上传的资料仅自己可见。
- **公共库（public）**：由超级管理员维护的系统级 team（`type='public'`），资料全平台可见；不写入 `team_members`，仅由可见性计算特判。
- **知识库本质**：team 是资料与向量的逻辑容器。资料上传后由 Agent 做结构化解析（切分、抽取章节/知识点、生成 Embedding），写入该 team 对应的 `material_chunks`；RAG 检索时只在「用户可见的 team 集合」内取片段，且 teacher team 仅取 `shared=true`，天然实现权限隔离。
- **解析自动化**：`POST /api/materials` 成功后由后端**异步**触发 Parser 任务（文件落对象存储 → 入队 → 解析 → 写 chunks → 更新 `parse_status`）；`parse_status` 为 `pending/parsing/done/failed`，前端据此展示进度。

### 6.4 核心模块

| 模块 | 职责 |
|------|------|
| Auth | 注册/登录、JWT 签发与刷新、角色 |
| Team | team 创建、成员管理、可见 team 集合计算 |
| User | 用户资料、角色、订阅状态 |
| Material | 资料 CRUD（归属 team）、`shared` 可见性、触发解析 |
| Learning | 学习记录、练习成绩、进度聚合 |
| Agent Gateway | 代理 Agent 请求，注入用户上下文与「可见 team 集合」 |

### 6.5 API 设计（REST 摘要）

| 方法 | 路径 | 说明 | 鉴权 |
|------|------|------|------|
| POST | `/api/auth/login` | 登录 | 否 |
| POST | `/api/auth/register` | 注册（默认 student，自动建私人 team） | 否 |
| POST | `/api/auth/refresh` | 刷新 token（httpOnly Cookie） | 是 |
| GET | `/api/teams` | 我可见的 team 列表（私人 + 已加入 + 公共） | 是 |
| POST | `/api/teams` | 老师创建学习小组 team（返回 `join_code`） | teacher |
| POST | `/api/teams/:id/join` | 学生凭 `join_code` 申请加入（→ pending） | student |
| POST | `/api/teams/:id/members/:uid/approve` | 老师审批成员加入 | teacher(owner) |
| GET | `/api/teams/:id/members` | 成员与待审批列表 | teacher(owner) |
| GET | `/api/teams/:id/materials` | team 内可见资料（自动过滤 `shared`） | 是 |
| POST | `/api/materials` | 上传资料到指定 team（异步触发解析） | 是 |
| GET | `/api/materials` | 资料列表（按可见 team 过滤） | 是 |
| GET | `/api/materials/:id` | 资料详情（含 `parse_status`） | 是 |
| PUT | `/api/materials/:id` | 更新资料 / 切 `shared`（变更触发重解析） | 是 |
| DELETE | `/api/materials/:id` | 删除资料（级联删 chunks） | 是 |
| GET | `/api/learning/records` | 我的学习记录 | 是 |
| POST | `/api/learning/exercise` | 提交练习 | 是 |
| POST | `/api/agent/chat` | AI 对话（SSE 流式，检索可见 team + `shared`） | 是 |
| POST | `/api/agent/plan` | 生成学习计划（落 `study_plans`） | 是 |
| POST | `/api/agent/quiz` | 生成测评（落 `exercises`） | 是 |
| GET | `/api/agent/sessions` | 我的会话列表 | 是 |

> 所有写接口受 RBAC 约束；`/api/agent/*` 由后端向 Agent 注入 `user_id`、订阅档位与「可见 team 集合 + `shared` 过滤谓词」，Agent 不直接读权限表，仅持 `material_chunks` 的**只读**凭证执行检索。

---

## 7. Agent 设计（Python + Google ADK + A2A）

### 7.1 多智能体角色

采用 **Orchestrator + 专用 Agent** 模式，Agent 间通过 **A2A 协议**（HTTP/JSON）协作：

| Agent | 职责 |
|-------|------|
| **Orchestrator** | 接收后端请求（含可见 team 集合），意图分类，拆解子任务，路由并聚合 |
| **Parser** | 资料结构化解析：切分、抽取章节/知识点、生成 Embedding，落对应 team |
| **Retriever** | 在「可见 team 集合」内做向量检索，重排，返回资料片段 |
| **Tutor** | 答疑讲解：基于检索片段 + 对话历史生成通俗讲解，附引用（team/资料/章节） |
| **Planner** | 学习计划：按目标/期限/水平生成结构化计划 |
| **Evaluator** | 测评与批改：出题、判分、给薄弱点建议 |
| **Memory** | 维护用户长期画像与偏好（Redis + PostgreSQL） |

### 7.2 A2A 交互示例（答疑）

```
后端 ──POST /agent/chat (可见 team 集合)──▶ Orchestrator
Orchestrator ──A2A task──▶ Retriever   (仅在该 team 集合内返回 top-k)
Orchestrator ──A2A task──▶ Tutor       (片段 + 问题 + 历史 → 回答)
Orchestrator ──SSE 流式──▶ 后端 ──▶ 前端
```

### 7.3 RAG 流程

1. 问题（+ 当前资料上下文）→ Embedding。
2. 在「可见 team 集合」的 `material_chunks` 内近似检索。
3. 重排 → 拼入 prompt。
4. LLM 生成回答，输出引用来源（team / material / chapter）。
5. 新问答写入对话历史（Redis 短期 + PostgreSQL 长期）。

### 7.4 上下文与记忆

- **短期上下文**：当前会话轮次存 Redis（TTL 可配）。
- **长期记忆**：用户画像、薄弱点、偏好存 PostgreSQL。
- **安全**：Agent 仅持 `material_chunks` 的只读凭证；检索谓词（可见 team 集合 + teacher team 仅 `shared=true`）由后端计算后下发，Agent 无法直接访问全库或权限表。

### 7.5 资料结构化解析与团队知识库

- 任意资料（老师 team / 学生私人 team / 公共库）上传后，后端**异步**触发 Agent 的 **Parser 任务**：文件落对象存储 → 切分 → 抽取结构 → 生成 Embedding → 写入该 team 的 `material_chunks`（更新 `parse_status`）。
- 检索时，后端根据用户身份算出**有效检索谓词**：`team_id IN (可见集合) AND (teams.type <> 'teacher' OR materials.shared = true)`，仅在该谓词内做向量检索——teacher team 的草稿（`shared=false`）绝不会被学生召回。
- 学生私人 team 的资料也会被解析进其私有知识库，使其 AI 答疑可基于「自己的资料」作答。

---

## 8. 数据层设计

### 8.1 PostgreSQL 核心表（DDL 摘要）

```sql
-- 用户与角色
CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  email         VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  display_name  VARCHAR(100),
  role          VARCHAR(20) NOT NULL DEFAULT 'student', -- student/teacher/super_admin
  subscription  VARCHAR(20) DEFAULT 'free',
  created_at    TIMESTAMPTZ DEFAULT now()
);

-- 团队 / 知识库
CREATE TABLE teams (
  id          BIGSERIAL PRIMARY KEY,
  name        VARCHAR(200) NOT NULL,
  type        VARCHAR(20) NOT NULL,   -- private(学生私有) / teacher(老师小组) / public(公共库)
  join_code   VARCHAR(20) UNIQUE,     -- 仅 teacher team 使用，学生凭码加入
  owner_id    BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ DEFAULT now()
);
-- 公共库为系统级单一虚拟 team（type='public'），由超级管理员维护，不写入 team_members

CREATE TABLE team_members (
  team_id     BIGINT REFERENCES teams(id),
  user_id     BIGINT REFERENCES users(id),
  role        VARCHAR(20) DEFAULT 'member',  -- member / co_teacher（owner 以 teams.owner_id 为准）
  status      VARCHAR(20) DEFAULT 'approved',-- pending(待审批) / approved(已加入)
  joined_at   TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

-- 学习资料（归属某个 team / 知识库）
CREATE TABLE materials (
  id          BIGSERIAL PRIMARY KEY,
  team_id     BIGINT REFERENCES teams(id),  -- 所属团队 / 知识库
  title       VARCHAR(300) NOT NULL,
  subject     VARCHAR(100),
  chapter     VARCHAR(100),
  tags        TEXT[],
  content     TEXT,                         -- 正文 / Markdown（解析后回填）
  file_type   VARCHAR(20),                  -- pdf / pptx / docx / md / image ...
  storage_key VARCHAR(512),                 -- 对象存储键（MinIO/S3）
  parse_status VARCHAR(20) DEFAULT 'pending', -- pending/parsing/done/failed
  parse_error VARCHAR(512),
  shared      BOOLEAN DEFAULT false,        -- 仅 teacher team 生效：是否对 team 学生成员可见
  owner_id    BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_mat_team ON materials(team_id);
CREATE INDEX idx_mat_shared ON materials(team_id, shared) WHERE shared = true;

CREATE TABLE learning_records (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  material_id BIGINT REFERENCES materials(id),
  duration_s  INT DEFAULT 0,
  progress    NUMERIC(5,2) DEFAULT 0,
  score       NUMERIC(5,2),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_lr_user ON learning_records(user_id);

CREATE TABLE agent_sessions (
  id          UUID PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  title       VARCHAR(200),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_as_user ON agent_sessions(user_id);

CREATE TABLE agent_messages (
  id          BIGSERIAL PRIMARY KEY,
  session_id  UUID REFERENCES agent_sessions(id),
  role        VARCHAR(20),
  content     TEXT,
  citations   JSONB,   -- [{team_id, material_id, chapter, chunk_idx}]
  created_at  TIMESTAMPTZ DEFAULT now()
);

-- 测评题目与作答（Evaluator）
CREATE TABLE exercises (
  id          BIGSERIAL PRIMARY KEY,
  material_id BIGINT REFERENCES materials(id),
  session_id  UUID REFERENCES agent_sessions(id),
  question    TEXT NOT NULL,
  answer_key  TEXT,
  difficulty  VARCHAR(20),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE quiz_attempts (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  exercise_id BIGINT REFERENCES exercises(id),
  choice      TEXT,
  is_correct  BOOLEAN,
  score       NUMERIC(5,2),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_qa_user ON quiz_attempts(user_id);

-- 学习计划（Planner）
CREATE TABLE study_plans (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  title       VARCHAR(200),
  goal        TEXT,
  deadline    DATE,
  items       JSONB,    -- [{date, task, done}]
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_sp_user ON study_plans(user_id);

-- 用户长期画像（Memory Agent）
CREATE TABLE user_profiles (
  user_id     BIGINT PRIMARY KEY REFERENCES users(id),
  weak_points TEXT[],  -- 薄弱知识点
  preferences JSONB,
  updated_at  TIMESTAMPTZ DEFAULT now()
);

-- Token 用量（成本归因 / 订阅额度）
CREATE TABLE token_usage (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  service     VARCHAR(20),  -- chat/plan/quiz
  prompt_tokens  INT,
  completion_tokens INT,
  total_tokens INT,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_tu_user ON token_usage(user_id, created_at);
```

### 8.2 pgvector 向量表（按 team 隔离）

```sql
CREATE EXTENSION IF NOT EXISTS vector;
-- Embedding 模型：使用 Google ADK 配套的 text-embedding 模型，维度 <DIM> 通过环境变量 EMBEDDING_DIM 注入，全库必须一致。

CREATE TABLE material_chunks (
  id          BIGSERIAL PRIMARY KEY,
  team_id     BIGINT REFERENCES teams(id),    -- 知识库归属，检索时按可见 team 过滤
  material_id BIGINT REFERENCES materials(id),
  chunk_idx   INT,
  content     TEXT,
  embedding   vector(<DIM>)                    -- 维度由 embedding 模型决定（如 Gemini 768 / 3072），经配置注入
);
CREATE INDEX idx_chunk_team ON material_chunks(team_id);
CREATE INDEX idx_chunk_vec ON material_chunks
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
-- 检索谓词（后端下发）：team_id IN (可见集合) AND (teams.type <> 'teacher' OR materials.shared = true)
```

### 8.3 Redis 缓存策略

| Key 模式 | 内容 | TTL |
|----------|------|-----|
| `session:{session_id}` | 当前对话上下文（短期记忆） | 30 min 闲置 |
| `team:visible:{user_id}` | 用户可见 team 集合（加速鉴权/检索） | 5 min |
| `cache:material:{id}` | 热点资料正文 | 10 min |
| `ratelimit:agent:{user_id}` | 对话限流计数 | 1 min |
| `user:profile:{user_id}` | 用户画像快照 | 5 min |

> **缓存失效**：当 `material.shared` 翻转、或 `team_members` 增删/审批状态变化时，主动删除 `team:visible:{user_id}` 与 `user:profile:{user_id}`，避免学生最多 5 分钟内脏读草稿或漏看新公开资料。

---

## 9. 安全设计

- **认证**：JWT（access + refresh）；refresh 用 **httpOnly + Secure Cookie**（优于 localStorage，抗 XSS）；SSE 流式对话通过握手首帧或 query 参数传递 access token（`EventSource` 不能自定义 Header）。
- **授权**：RBAC + team 可见性 + 行级隔离；资料可见性在 repository 层强制，且 teacher team 仅返回 `shared=true`。
- **Agent 边界**：后端向 Agent 下发「可见 team 集合 + `shared` 过滤谓词」；Agent 仅持 `material_chunks` 的**只读** DB 角色，无法直接访问权限表或全库。
- **传输**：全站 HTTPS；SSE/WebSocket 同样鉴权。
- **内容安全**：K12 场景下对用户输入与模型输出做内容审核网关（关键词 + 模型护栏），防止不当内容。
- **限流**：Agent 对话按用户维度限流。
- **日志**：不记录明文密码与 token；对话日志脱敏。

---

## 10. 部署架构（草案）

```
docker-compose:
  postgres  (16 + pgvector)
  redis     (7)
  backend   (Go 容器)
  agent     (Python 容器)
  frontend  (静态构建 → Nginx / CDN)
```

- 前端静态资源由 Nginx 托管，API 反代到后端。
- 后端与 Agent 内部互通；Agent 不暴露公网。
- 可选 K8s：backend/agent 独立 HPA 扩缩容。

---

## 11. 可观测性

- 结构化日志（JSON）统一采集。
- 监控：QPS、P95 延迟、Agent 成功率、Token 消耗、解析任务队列。
- 追踪：前端 → 后端 → Agent → pgvector 用 trace id 串联。
- 告警：Agent 错误率、DB 连接池、Redis 内存阈值。

---

## 12. 扩展性与后续演进

- 多租户（学校/机构）隔离，team 可归属租户。
- Agent 能力市场（可插拔新 Agent）。
- 跨 team 检索授权（如学生授权学伴读多个知识库）。
- 评测闭环：用学习成效反哺 Planner/Evaluator。
- 成本优化：缓存命中率、模型路由。
