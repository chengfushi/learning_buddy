# 数据库文档（learning_buddy）

> 版本：v0.4 · 最后更新：2026-07-18
> 配套：系统设计 `docs/system-design.md` §8 · 工程规范 `docs/engineering-standards.md`
> 关系库：**PostgreSQL 16 + pgvector**；缓存：**Redis 7**；文件：**MinIO/S3**

---

## 1. 总览

系统以「**团队 = 知识库**」组织资料与权限。三类角色：

- `student`：自动拥有私人 `team`；可加入老师的 `team`；默认可看公共库。
- `teacher`：创建 `teacher` 类型 `team`，仅自己可上传资料，逐份 `shared` 控制对学生可见。
- `super_admin`：维护系统级 `public` `team`（全平台可见，**不写入 `team_members`**）。

| 分类 | 表 |
|------|----|
| 账户/权限 | `users` · `teams` · `team_members` |
| 资料/知识库 | `materials` · `material_chunks`（向量） |
| 学习/对话 | `learning_records` · `agent_sessions` · `agent_messages` |
| 测评/计划/画像 | `exercises` · `quiz_attempts` · `study_plans` · `user_profiles` |
| 计费 | `token_usage` |

---

## 2. 表结构

### 2.1 users（用户）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `id` | BIGSERIAL | PK |
| `email` | VARCHAR | 登录账号（唯一） |
| `role` | VARCHAR(20) NOT NULL DEFAULT `student` | `student` / `teacher` / `super_admin` |
| `subscription` | VARCHAR(20) DEFAULT `free` | `free` / `pro`（接限流与功能档位） |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

### 2.2 teams（团队 / 知识库）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `id` | BIGSERIAL | PK |
| `name` | VARCHAR(200) NOT NULL | 团队名 |
| `type` | VARCHAR(20) NOT NULL | `private`（学生私有）/ `teacher`（老师小组）/ `public`（公共库） |
| `join_code` | VARCHAR(20) UNIQUE | 仅 `teacher` team；学生凭码加入 |
| `owner_id` | BIGINT | FK → `users(id)`（归属者；`team_members` 不再冗余 owner 角色） |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

> 公共库为系统级单一虚拟 team（`type='public'`），由超级管理员维护，**不落 `team_members` 行**（在「可见 team 集合」计算中特判）。

### 2.3 team_members（成员关系，N:M）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `team_id` | BIGINT | FK → `teams(id)` |
| `user_id` | BIGINT | FK → `users(id)` |
| `role` | VARCHAR(20) DEFAULT `member` | `member` / `co_teacher`（owner 以 `teams.owner_id` 为准） |
| `status` | VARCHAR(20) DEFAULT `approved` | `pending`（待审批）/ `approved`（已加入） |
| `joined_at` | TIMESTAMPTZ | DEFAULT now() |
| — | — | PK(`team_id`, `user_id`) |

关系：`teams 1 — N team_members N — 1 users`。

### 2.4 materials（学习资料）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `id` | BIGSERIAL | PK |
| `team_id` | BIGINT | FK → `teams(id)`（所属团队 / 知识库） |
| `title` | VARCHAR(300) NOT NULL | 标题 |
| `subject` | VARCHAR(100) | 学科 |
| `chapter` | VARCHAR(100) | 章节 |
| `tags` | TEXT[] | 标签 |
| `content` | TEXT | 正文 / Markdown（解析后回填） |
| `file_type` | VARCHAR(20) | `pdf` / `pptx` / `docx` / `md` / `image` … |
| `storage_key` | VARCHAR(512) | 对象存储键（MinIO/S3） |
| `parse_status` | VARCHAR(20) DEFAULT `pending` | `pending` / `parsing` / `done` / `failed` |
| `parse_error` | VARCHAR(512) | 解析失败原因 |
| `parse_generation` | BIGINT DEFAULT `1` | 每次重新入队递增；Backend/Agent 以此拒绝陈旧 worker 完成或写入 |
| `shared` | BOOLEAN DEFAULT false | **仅 `teacher` team 生效**：是否对 team 学生成员可见 |
| `owner_id` | BIGINT | FK → `users(id)` |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_mat_team(team_id)`、`idx_mat_shared(team_id, shared) WHERE shared = true`。

### 2.5 material_chunks（向量片段，pgvector）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `id` | BIGSERIAL | PK |
| `team_id` | BIGINT | FK → `teams(id)`（知识库归属，检索时按可见 team 过滤） |
| `material_id` | BIGINT | FK → `materials(id)` |
| `chunk_idx` | INT | 片段序号；与 `material_id` 组成唯一索引，保证解析重试幂等 |
| `content` | TEXT | 片段文本 |
| `embedding` | vector(1024) | 当前由 `EMBEDDING_DIM=1024` 与启动断言统一；变更维度必须新增迁移 |

索引：
- `idx_chunk_team(team_id)`
- `uq_material_chunks_material_idx(material_id, chunk_idx)`（唯一）
- `idx_chunk_vec`：`USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`

> **维度一致性**：全库必须统一维度，启动时断言与库表一致；建库前先 `CREATE EXTENSION IF NOT EXISTS vector;`。

### 2.6 learning_records（学习记录）

| 字段 | 类型 | 约束 / 说明 |
|------|------|------------|
| `id` | BIGSERIAL | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)`（行级隔离） |
| `material_id` | BIGINT | FK → `materials(id)` |
| `duration_s` | INT DEFAULT 0 | 学习时长（秒） |
| `progress` | NUMERIC(5,2) DEFAULT 0 | 完成度 |
| `score` | NUMERIC(5,2) | 测验得分 |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_lr_user(user_id)`。

### 2.7 agent_sessions / agent_messages（对话）

| agent_sessions | 类型 | 说明 |
|------|------|------|
| `id` | UUID | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)` |
| `material_id` | BIGINT | FK → `materials(id)` ON DELETE CASCADE；`NULL` 表示全局会话，否则固定在该资料作用域 |
| `title` | VARCHAR(200) | 会话标题 |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

| agent_messages | 类型 | 说明 |
|------|------|------|
| `id` | BIGSERIAL | PK |
| `session_id` | UUID | FK → `agent_sessions(id)` ON DELETE CASCADE |
| `role` | VARCHAR(20) | `user` / `assistant` / `system` |
| `content` | TEXT | 消息内容 |
| `citations` | JSONB | `[{team_id, material_id, chapter, chunk_idx}]`（引用来源） |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_as_user(user_id)`、`idx_as_user_material_created(user_id, material_id, created_at DESC)`。

### 2.8 exercises / quiz_attempts（测评，Evaluator）

| exercises | 类型 | 说明 |
|------|------|------|
| `id` | BIGSERIAL | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)`；题目所有者 |
| `material_id` | BIGINT | FK → `materials(id)` |
| `session_id` | UUID | FK → `agent_sessions(id)` ON DELETE SET NULL |
| `question` | TEXT NOT NULL | 题目 |
| `answer_key` | TEXT | 参考答案 |
| `difficulty` | VARCHAR(20) | 难度 |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

| quiz_attempts | 类型 | 说明 |
|------|------|------|
| `id` | BIGSERIAL | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)` |
| `exercise_id` | BIGINT | FK → `exercises(id)` |
| `choice` | TEXT | 作答 |
| `is_correct` | BOOLEAN | 是否正确 |
| `score` | NUMERIC(5,2) | 得分 |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_exercises_user(user_id)`、`idx_qa_user(user_id)`。

### 2.9 study_plans（学习计划，Planner）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | BIGSERIAL | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)` |
| `title` | VARCHAR(200) | 计划标题 |
| `goal` | TEXT | 目标 |
| `deadline` | DATE | 截止 |
| `items` | JSONB | `[{date, task, done}]` |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_sp_user(user_id)`。

### 2.10 user_profiles（长期画像，Memory Agent）

| 字段 | 类型 | 说明 |
|------|------|------|
| `user_id` | BIGINT | PK + FK → `users(id)` |
| `weak_points` | TEXT[] | 薄弱知识点 |
| `preferences` | JSONB | 偏好 |
| `updated_at` | TIMESTAMPTZ | DEFAULT now() |

### 2.11 token_usage（成本归因 / 订阅额度）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | BIGSERIAL | PK |
| `user_id` | BIGINT NOT NULL | FK → `users(id)` |
| `service` | VARCHAR(20) | `chat` / `plan` / `quiz` |
| `prompt_tokens` | INT | 输入 token |
| `completion_tokens` | INT | 输出 token |
| `total_tokens` | INT | 合计 |
| `created_at` | TIMESTAMPTZ | DEFAULT now() |

索引：`idx_tu_user(user_id, created_at)`。

---

## 3. 关系图（实体概览）

```
users ──< team_members >── teams ──< materials ──< material_chunks (vector)
                           │
users ──< learning_records >── materials
users ──< agent_sessions ──< agent_messages
materials ──< agent_sessions（可选资料作用域）
users ──< quiz_attempts >── exercises ──< materials
users ──< study_plans
users ──< user_profiles (1:1)
users ──< token_usage
```

基数：`teams 1—N team_members N—1 users`；`teams 1—N materials`；`materials 1—N material_chunks`。

---

## 4. 团队知识库检索谓词（权限隔离在检索层生效）

RAG 向量检索时，Backend repository 先计算「用户可见 team 集合」，再在同一数据库查询中直接执行如下谓词；谓词不会下发给 Agent：

```sql
team_id IN (可见 team 集合)
AND (
  teams.owner_id = :user_id
  OR teams.type <> 'teacher'
  OR materials.shared = true
)
```

- 可见集合 = 自己拥有的私人/teacher team + 已 `approved` 加入的 teacher team + 公共库（`type='public'`，特判不查 `team_members`）。
- team owner 可读取自己团队中的全部资料；其他用户读取 teacher team 时必须同时满足成员已审批且 `shared=true`。
- `teacher` team 中 `shared=false` 的备课草稿**绝不会被学生召回**。
- 任何权限改动（翻转 `shared`、成员审批）后，主动删除 `team:visible:{user_id}` 缓存（见 §5）。

---

## 5. Redis 缓存键

| Key 模式 | 内容 | TTL |
|----------|------|-----|
| `session:{session_id}` | 对话上下文（短期记忆） | 30 min 闲置 |
| `team:visible:{user_id}` | 用户可见 team 集合（加速鉴权/检索） | 5 min |
| `cache:material:{id}` | 热点资料正文 | 10 min |
| `ratelimit:agent:{user_id}` | 对话限流计数 | 1 min |
| `user:profile:{user_id}` | 用户画像快照 | 5 min |

> **缓存失效**：`material.shared` 翻转 / `team_members` 增删或审批变化时，删除 `team:visible:{user_id}` 与 `user:profile:{user_id}`。

---

## 6. 迁移与初始化建议

1. 启用扩展：`CREATE EXTENSION IF NOT EXISTS vector;`（pgvector）。
2. 按 §2 顺序建表（先 `users`/`teams`，再依赖表）。
3. 初始化系统级 `public` team（一次性种子）。
4. 为新用户自动创建 `private` team（注册流程中）。
5. 全库 `EMBEDDING_DIM` 必须一致；应用启动时断言向量列维度与配置相符。
