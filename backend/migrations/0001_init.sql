-- learning_buddy 初始化迁移 (PG17 + pgvector)
-- 关系库：PostgreSQL 17；向量：pgvector 0.8.x
-- 初始基线使用 vector(768)；0004 会升级为当前统一的 vector(1024)。
-- 注意：本迁移在本地直接由 psql 执行；CI/容器环境可改用 golang-migrate/atlas。

-- 1. 启用扩展（必须最先执行）
CREATE EXTENSION IF NOT EXISTS vector;

-- 2. 用户与角色
CREATE TABLE IF NOT EXISTS users (
  id            BIGSERIAL PRIMARY KEY,
  email         VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  display_name  VARCHAR(100),
  role          VARCHAR(20) NOT NULL DEFAULT 'student', -- student/teacher/super_admin
  subscription  VARCHAR(20) DEFAULT 'free',             -- free/pro
  created_at    TIMESTAMPTZ DEFAULT now()
);

-- 3. 团队 / 知识库
CREATE TABLE IF NOT EXISTS teams (
  id          BIGSERIAL PRIMARY KEY,
  name        VARCHAR(200) NOT NULL,
  type        VARCHAR(20) NOT NULL,   -- private(学生私有)/teacher(老师小组)/public(公共库)
  join_code   VARCHAR(20) UNIQUE,     -- 仅 teacher team 使用，学生凭码加入
  owner_id    BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ DEFAULT now()
);
-- 公共库为系统级单一虚拟 team（type='public'），由超级管理员维护，不写入 team_members

CREATE TABLE IF NOT EXISTS team_members (
  team_id     BIGINT REFERENCES teams(id) ON DELETE CASCADE,
  user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
  role        VARCHAR(20) DEFAULT 'member',  -- member / co_teacher（owner 以 teams.owner_id 为准）
  status      VARCHAR(20) DEFAULT 'approved',-- pending(待审批) / approved(已加入)
  joined_at   TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

-- 4. 学习资料（归属某个 team / 知识库）
CREATE TABLE IF NOT EXISTS materials (
  id           BIGSERIAL PRIMARY KEY,
  team_id      BIGINT REFERENCES teams(id) ON DELETE CASCADE,
  title        VARCHAR(300) NOT NULL,
  subject      VARCHAR(100),
  chapter      VARCHAR(100),
  tags         TEXT[],
  content      TEXT,                         -- 正文 / Markdown（解析后回填）
  file_type    VARCHAR(20),                  -- pdf/pptx/docx/md/image/txt
  storage_key  VARCHAR(512),                 -- 本地/对象存储键
  parse_status VARCHAR(20) DEFAULT 'pending',-- pending/parsing/done/failed
  parse_error  VARCHAR(512),
  shared       BOOLEAN DEFAULT false,        -- 仅 teacher team 生效：是否对 team 学生成员可见
  owner_id     BIGINT REFERENCES users(id),
  created_at   TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mat_team ON materials(team_id);
CREATE INDEX IF NOT EXISTS idx_mat_shared ON materials(team_id, shared) WHERE shared = true;

-- 5. 向量片段（pgvector，按 team 隔离）
CREATE TABLE IF NOT EXISTS material_chunks (
  id          BIGSERIAL PRIMARY KEY,
  team_id     BIGINT REFERENCES teams(id) ON DELETE CASCADE,
  material_id BIGINT REFERENCES materials(id) ON DELETE CASCADE,
  chunk_idx   INT,
  content     TEXT,
  embedding   vector(768)                    -- 维度由 embedding 模型决定，全库统一 768
);
CREATE INDEX IF NOT EXISTS idx_chunk_team ON material_chunks(team_id);
CREATE INDEX IF NOT EXISTS idx_chunk_mat ON material_chunks(material_id);
CREATE INDEX IF NOT EXISTS idx_chunk_vec ON material_chunks
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- 6. 学习记录
CREATE TABLE IF NOT EXISTS learning_records (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  material_id BIGINT REFERENCES materials(id),
  duration_s  INT DEFAULT 0,
  progress    NUMERIC(5,2) DEFAULT 0,
  score       NUMERIC(5,2),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_lr_user ON learning_records(user_id);
CREATE INDEX IF NOT EXISTS idx_lr_mat ON learning_records(material_id);

-- 7. 对话
CREATE TABLE IF NOT EXISTS agent_sessions (
  id          UUID PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  title       VARCHAR(200),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_as_user ON agent_sessions(user_id);

CREATE TABLE IF NOT EXISTS agent_messages (
  id          BIGSERIAL PRIMARY KEY,
  session_id  UUID REFERENCES agent_sessions(id) ON DELETE CASCADE,
  role        VARCHAR(20),                  -- user/assistant/system
  content     TEXT,
  citations   JSONB,                         -- [{team_id,material_id,chapter,chunk_idx}]
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_am_session ON agent_messages(session_id);

-- 8. 测评题目与作答（Evaluator）
CREATE TABLE IF NOT EXISTS exercises (
  id          BIGSERIAL PRIMARY KEY,
  material_id BIGINT REFERENCES materials(id),
  session_id  UUID REFERENCES agent_sessions(id),
  question    TEXT NOT NULL,
  options     JSONB,                         -- 选择题选项
  answer_key  TEXT,
  difficulty  VARCHAR(20),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS quiz_attempts (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  exercise_id BIGINT REFERENCES exercises(id),
  choice      TEXT,
  is_correct  BOOLEAN,
  score       NUMERIC(5,2),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_qa_user ON quiz_attempts(user_id);
CREATE INDEX IF NOT EXISTS idx_qa_exercise ON quiz_attempts(exercise_id);

-- 9. 学习计划（Planner）
CREATE TABLE IF NOT EXISTS study_plans (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  title       VARCHAR(200),
  goal        TEXT,
  deadline    DATE,
  items       JSONB,    -- [{date, task, done}]
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sp_user ON study_plans(user_id);

-- 10. 用户长期画像（Memory Agent）
CREATE TABLE IF NOT EXISTS user_profiles (
  user_id     BIGINT PRIMARY KEY REFERENCES users(id),
  weak_points TEXT[],  -- 薄弱知识点
  preferences JSONB,
  updated_at  TIMESTAMPTZ DEFAULT now()
);

-- 11. Token 用量（成本归因 / 订阅额度）
CREATE TABLE IF NOT EXISTS token_usage (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  service     VARCHAR(20),  -- chat/plan/quiz
  prompt_tokens  INT,
  completion_tokens INT,
  total_tokens INT,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tu_user ON token_usage(user_id, created_at);

-- 12. 阅读笔记 / 标注（F3 阅读器）
CREATE TABLE IF NOT EXISTS material_notes (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id),
  material_id BIGINT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
  content     TEXT NOT NULL,                 -- 笔记正文
  quote       TEXT,                           -- 标注的原文片段
  created_at  TIMESTAMPTZ DEFAULT now(),
  updated_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_note_user ON material_notes(user_id);
CREATE INDEX IF NOT EXISTS idx_note_mat ON material_notes(material_id);
