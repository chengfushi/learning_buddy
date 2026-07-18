# 智能学伴系统 · 系统设计文档

> 版本：v0.5 · 状态：设计稿（修订） · 最后更新：2026-07-17
> 变更：v0.5 增加 RAG v2 的对象存储解析管道、混合召回、Rerank、父资料扩展、反馈闭环和影子索引灰度；资料可见性仍由 Backend repository 作为唯一真源。

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
│  · repository 权限过滤 + pgvector top-k → 已授权 chunks        │
└──────┬──────────────────────┬───────────────────┬───────────┘
       │                      │                   │
       ▼                      ▼                   ▼
┌──────────────┐     ┌────────────────┐   ┌──────────────────────┐
│ PostgreSQL   │     │   Redis 7      │   │  Agent 层 (Python)    │
│ + pgvector   │     │ 会话/热点/限流  │   │  Google ADK + A2A     │
│ teams/资料/向量│     │               │   │  Orchestrator+Parser   │
└──────────────┘     └────────────────┘   │  +Tutor+Planner+Evaluator│
                                          └──────────┬───────────┘
                                                     │ A2A (HTTP/JSON)
                                          ┌──────────▼───────────┐
                                          │ Parser 写当前代次 chunks│
                                          │ 生成任务只消费 Backend  │
                                          │ 已授权 chunks           │
                                          └──────────────────────┘
```

### 3.1 分层职责

| 层 | 职责 | 不负责 |
|----|------|--------|
| 前端 | 交互、状态、渲染、调用后端 API | 业务逻辑、数据存储、模型推理 |
| 后端 | 鉴权、Team/RBAC、资料 CRUD、资料权限过滤、pgvector 检索、Agent 请求代理 | 模型推理 |
| Agent | 意图理解、资料解析、多 Agent 协作、基于已授权 chunks 生成回答 | 用户体系、资料权限判定与向量检索 |
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

- **智能答疑**：选中文本/整页提问，Backend repository 先在用户可见范围内完成 RAG 检索，Agent 仅基于已授权 chunks 回答并附引用；引用可打开资料并定位到对应 PDF 页码或图片资产。
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

- **Middleware**：JWT 鉴权、RBAC 校验、限流、日志/追踪。资料可见性计算与 pgvector 检索收口在 repository。
- **Agent Client**：封装对 Agent 的 HTTP 调用，含超时、重试、SSE 流式转发；生成请求只携带 repository 已过滤的 chunks，不下发权限谓词。

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
- 关键规则：**每个 team 即一个知识库**；资料读取严格限定在「用户可见 team 集合」内。owner 可管理自己 teacher team 的草稿，其他用户必须为 `approved` 成员且资料 `shared=true`；详情、team 列表与笔记复用同一 repository scope。

### 6.3 团队与知识库（Team & Knowledge Base）

- **老师的 team（学习小组）**：老师创建（生成 `join_code`），成员为学生；仅老师可上传资料；每份资料有 `shared` 开关——开启后 team 内 `approved` 学生可见，关闭则仅老师自己可见（备课/草稿）。
- **学生的私人 team**：系统为每个学生注册时自动生成一个私有 team，学生自己上传的资料仅自己可见。
- **公共库（public）**：由超级管理员维护的系统级 team（`type='public'`），资料全平台可见；不写入 `team_members`，仅由可见性计算特判。
- **知识库本质**：team 是资料与向量的逻辑容器。资料上传后由 Agent Parser 做结构化解析（切分、抽取章节/知识点、生成 Embedding），通过最小写权限账号写入 `material_chunks`；问答/规划/测评的 RAG 可见性谓词与 pgvector top-k 均由 Backend repository 执行，Agent 只消费已授权 chunks。
- **解析自动化**：`POST /api/materials` 成功后由后端**异步**触发 Parser 任务（文件落对象存储 → 入队 → 解析 → 写 chunks → 更新 `parse_status`）；`parse_status` 为 `pending/parsing/done/failed`，前端据此展示进度。

### 6.4 核心模块

| 模块 | 职责 |
|------|------|
| Auth | 注册/登录、JWT 签发与刷新、角色 |
| Team | team 创建、成员管理、可见 team 集合计算 |
| User | 用户资料、角色、订阅状态 |
| Material | 资料 CRUD（归属 team）、`shared` 可见性、触发解析 |
| Learning | 学习记录、练习成绩、进度聚合 |
| Agent Gateway | 调用 repository 检索已授权 chunks，代理 Agent 生成请求与 SSE 转发 |

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
| POST | `/api/materials/:id/retry` | 重试 `failed` 解析任务（需 team 写权限） | 是 |
| DELETE | `/api/materials/:id` | 删除资料（级联删 chunks） | 是 |
| GET | `/api/learning/records` | 我的学习记录 | 是 |
| POST | `/api/learning/exercise` | 提交练习 | 是 |
| POST | `/api/agent/chat` | AI 对话（Backend 检索已授权 chunks，Agent SSE 流式生成） | 是 |
| POST | `/api/agent/plan` | 生成学习计划（落 `study_plans`） | 是 |
| POST | `/api/agent/quiz` | 生成测评（落 `exercises`） | 是 |
| GET | `/api/agent/sessions` | 我的会话列表 | 是 |

> 所有写接口受 RBAC 约束；`/api/agent/*` 先由 Backend repository 应用统一可见性谓词并执行 pgvector top-k，再向 Agent 下发已授权 chunks。Agent Parser 仅持解析所需的最小数据库权限，不可读取用户、成员或认证表。

---

## 7. Agent 设计（Python + Google ADK + A2A）

### 7.1 多智能体角色

采用 **Orchestrator + 专用 Agent** 模式，Agent 间通过 **A2A 协议**（HTTP/JSON）协作：

| Agent | 职责 |
|-------|------|
| **Orchestrator** | 接收后端请求（含 repository 已过滤 chunks），意图分类，路由并聚合 |
| **Parser** | 资料结构化解析：切分、抽取章节/知识点、生成 Embedding，落对应 team |
| **Backend Retriever** | repository 应用统一资料可见性谓词后执行 pgvector top-k，返回已授权片段 |
| **Tutor** | 答疑讲解：基于检索片段 + 对话历史生成通俗讲解，附引用（team/资料/章节） |
| **Planner** | 学习计划：按目标/期限/水平生成结构化计划 |
| **Evaluator** | 测评与批改：出题、判分、给薄弱点建议 |
| **Memory** | 维护用户长期画像与偏好（Redis + PostgreSQL） |

### 7.2 A2A 交互示例（答疑）

```
后端 repository ──可见性谓词 + pgvector top-k──▶ 已授权 chunks
后端 ──POST /agent/chat (chunks)──▶ Orchestrator
Orchestrator ──A2A task──▶ Tutor       (片段 + 问题 + 历史 → 回答)
Orchestrator ──SSE 流式──▶ 后端 ──▶ 前端
```

### 7.3 RAG 流程

1. Backend 验证会话归属并读取最近 20 条消息，Agent `/analyze-query` 仅对指代性追问改写，同时生成关键词和 1024 维 Embedding。
2. Backend repository 在 `VisibleMaterialsScope` 内分别执行 HNSW 向量 Top-30 与全文/Trigram 词法 Top-30，以 RRF(k=60) 融合为 20 条。指定 `material_id` 仍先验证可见性。
3. Agent `/rerank` 对已授权候选使用 `qwen3-rerank` 取 Top-8；Backend 再次套用可见性谓词，按父资料扩展同章节和相邻正文块，限制为 3 份资料、8 块、12k Token。
4. LLM 回答原始问题；引用只能来自实际上下文块，Backend 使用可信数据库字段重建 material/chunk/asset 引用。
5. 原始/改写查询、阶段耗时、候选分数和反馈写入 PostgreSQL；Redis 只缓存查询分析、Embedding 与候选内容哈希对应的 Rerank 结果。

> **R6 超时与降级**：总检索目标 P95 ≤ 2.5 秒；降级顺序为 Rewrite→原问题、Embedding→词法召回、Rerank→RRF、OCR→仅展示图片、无候选→有据拒答。Agent 对 Tutor 设置 `30s` 预算，SSE 使用 `trace_id` 关联各阶段。

### 7.4 上下文与记忆

- **短期上下文**：Backend 从 PostgreSQL 读取当前用户会话最近 20 条消息；前端按全局/资料作用域分别复用 `session_id`。
- **长期记忆**：用户画像、薄弱点、偏好存 PostgreSQL。
- **安全**：可见性谓词与向量 SQL 只存在于 Backend repository；Agent 不提供 `/retrieve`，请求契约只接收已授权 chunks。Parser 的数据库凭证仅需读取资料归属、更新正文并读写 chunks，不得访问用户、成员与认证数据。

### 7.5 资料结构化解析与团队知识库

- 任意资料上传后，后端**异步**触发 Agent Parser：Backend repository 独占 `materials.parse_status` 状态机，并为每次重新入队递增 `parse_generation`。worker 完成、Agent 替换 chunks 都必须匹配当前代次；Agent 再以 `pg_advisory_xact_lock(material_id)` 串行化写入并校验状态仍为 `parsing`。唯一索引 `(material_id, chunk_idx)` 阻止重复片段，迟到请求无法覆盖新内容或最终状态。
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
  parse_generation BIGINT DEFAULT 1,          -- 每次重新入队递增，隔离陈旧 worker
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
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  material_id BIGINT REFERENCES materials(id),
  session_id  UUID REFERENCES agent_sessions(id),
  question    TEXT NOT NULL,
  options     JSONB,
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
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE material_chunks (
  id            BIGSERIAL PRIMARY KEY,
  team_id       BIGINT REFERENCES teams(id),
  material_id   BIGINT REFERENCES materials(id),
  index_version VARCHAR(40) NOT NULL,
  kind          VARCHAR(20) NOT NULL,           -- body/summary/question/image
  chunk_idx     INT NOT NULL,
  heading_path  TEXT,
  page_number   INT,
  token_count   INT NOT NULL DEFAULT 0,
  lexical_text  TEXT NOT NULL DEFAULT '',
  lexical_tsv   TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', lexical_text)) STORED,
  content       TEXT NOT NULL,
  embedding     vector(1024),
  asset_id      BIGINT REFERENCES material_assets(id)
);
CREATE UNIQUE INDEX uq_material_chunks_version_kind_idx
  ON material_chunks(material_id, index_version, kind, chunk_idx);
CREATE INDEX idx_chunk_vec_hnsw ON material_chunks
  USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 128);
CREATE INDEX idx_chunk_lexical_tsv ON material_chunks USING gin (lexical_tsv);
CREATE INDEX idx_chunk_lexical_trgm ON material_chunks USING gin (lexical_text gin_trgm_ops);
-- 检索和父资料扩展均复用 Backend repository 的 VisibleMaterialsScope。
```

完整 RAG v2 表结构以 `backend/migrations/0008_rag_v2.sql` 为准；上线与回滚步骤见 `docs/rag-v2-production.md`。

### 8.3 Redis 缓存策略

| Key 模式 | 内容 | TTL |
|----------|------|-----|
| `rag:analysis:{hash}` | Query Rewrite、关键词与 Embedding | 30 min |
| `rag:rerank:{hash}` | 模型、Top-N 与候选内容哈希对应的 Rerank 结果 | 60 min |
| `team:visible:{user_id}` | 用户可见 team 集合（加速鉴权/检索） | 5 min |
| `cache:material:{id}` | 热点资料正文 | 10 min |
| `ratelimit:agent:{user_id}` | 对话限流计数 | 1 min |
| `user:profile:{user_id}` | 用户画像快照 | 5 min |

> **缓存失效**：当 `material.shared` 翻转、或 `team_members` 增删/审批状态变化时，主动删除 `team:visible:{user_id}` 与 `user:profile:{user_id}`，避免学生最多 5 分钟内脏读草稿或漏看新公开资料。

> **当前实现（R7）**：上述 Redis key 仍是目标设计，Go Backend 目前没有可见集缓存，所有可见性查询直接以 repository + PostgreSQL 为真源，因此 shared 翻转和成员审批会在下一次查询立即生效。未来启用缓存时，失效逻辑必须集中在 repository 写方法内，并通过现有即时可见性集成测试。

---

## 9. 安全设计

- **认证**：JWT（access + refresh）；SSE 流式对话强制使用 `fetch + ReadableStream`，access token 仅通过 `Authorization: Bearer` Header 传递，禁止 `EventSource` 或 URL query token，避免 token 进入访问日志。
- **授权**：RBAC + team 可见性 + 行级隔离；资料可见性在 repository 层强制，且 teacher team 仅返回 `shared=true`。
- **Agent 边界**：后端 repository 完成权限过滤与向量检索，只向 Agent 下发已授权 chunks；所有业务请求必须携带 `X-Agent-Token` 共享密钥，Agent 使用常量时间比较并默认拒绝无凭证调用。Agent 无权访问用户、成员或认证数据。
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
- 后端与 Agent 仅在容器内部网络互通；Agent 不映射宿主机端口、不暴露公网，共享密钥通过 `AGENT_SHARED_SECRET` 注入且两端必须一致。
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
