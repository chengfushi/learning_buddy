# AI Learning Companion · System Design Document

> Version: v0.3 · Status: Design Draft (Revised) · Last Updated: 2026-07-11
> Changelog: v0.3 fixes from requirements review — RAG adds `shared` mandatory filter, Agent read-only credentials, class code + approval status, file/parse state machine, embedding dimension configuration, four missing tables (exercises/quiz_attempts/study_plans/user_profiles/token_usage), subscription tier linked to rate limiting, K12 content safety, roadmap reorder

---

## 1. Document Information

| Item | Content |
|------|---------|
| Project Name | AI Learning Companion |
| Document Type | System Architecture / Detailed Design |
| Target Audience | R&D, Architects, Tech Leads, Product |
| Tech Stack | React · Go/Gin · Python/Google ADK/A2A · PostgreSQL + pgvector · Redis |

---

## 2. System Overview

### 2.1 Background & Goals

The AI Learning Companion leverages large language models and multi-agent capabilities to deliver two closed-loop experiences for students:

- **Self-Directed Learning**: Students browse and practice materials at their own pace, with the system tracking their learning path and providing feedback.
- **AI-Assisted Learning**: Students ask the Agent questions, receive explanations, generate study plans, and take assessments — all powered by **Team-based Knowledge Base** (RAG) for personalized, traceable assistance.

The system organizes materials and permissions around the concept of **"Team = Knowledge Base"**: teachers create study groups, students have private teams, and super admins maintain a public library. All materials are structurally parsed by the Agent upon upload and stored in the corresponding team's vector/structured storage.

### 2.2 Design Principles

1. **Separation of Concerns**: Frontend (interaction) / Backend (business/data) / Agent (intelligence) — three layers independently deployed.
2. **Team as Knowledge Base**: Materials, vectors, and retrieval permissions all bounded by team, with isolation naturally enforced at the retrieval layer.
3. **RAG-First**: Agent answers are preferentially based on the controlled material repository, reducing hallucinations and ensuring traceability.
4. **Stateful but Controllable**: Conversation context cached in Redis; long-term memory persisted to PostgreSQL.
5. **Least Privilege**: RBAC + team visibility + row-level isolation — students can only access authorized materials and their own data.

---

## 3. Overall Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     User / Browser                           │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTPS / REST + WebSocket(SSE)
┌───────────────────────────▼─────────────────────────────────┐
│              Frontend Layer  (React + TypeScript)             │
│  Library/Teams │ AI Companion │ Learning Center │ Profile     │
└───────────────────────────┬─────────────────────────────────┘
                            │ REST API (JWT Auth)
┌───────────────────────────▼─────────────────────────────────┐
│        Backend Layer  (Go + Gin) — Business & Data Gateway   │
│  Auth/AuthZ │ Team&RBAC │ Material CRUD │ Learning Records   │
│  · Computes "visible team set" → passes to Agent for filter  │
└──────┬──────────────────────┬───────────────────┬───────────┘
       │                      │                   │
       ▼                      ▼                   ▼
┌──────────────┐     ┌────────────────┐   ┌──────────────────────┐
│ PostgreSQL   │     │   Redis 7      │   │  Agent Layer (Python)  │
│ + pgvector   │     │ Session/Hot/RL │   │  Google ADK + A2A      │
│ teams/mat/vec│     │               │   │  Orchestrator+Retriever │
└──────────────┘     └────────────────┘   │  +Tutor+Planner+Eval   │
                                          └──────────┬───────────┘
                                                     │ A2A (HTTP/JSON)
                                          ┌──────────▼───────────┐
                                          │ Retriever searches    │
                                          │ within "visible team   │
                                          │ set + shared predicate"│
                                          │ pgvector (read-only)   │
                                          └──────────────────────┘
```

### 3.1 Layer Responsibilities

| Layer | Responsibility | NOT Responsible For |
|-------|---------------|---------------------|
| Frontend | Interaction, state, rendering, calling backend APIs | Business logic, data storage, model inference |
| Backend | Authentication, Team/RBAC, Material CRUD, computing visible team set, Agent request proxy | Model inference, vector retrieval implementation |
| Agent | Intent understanding, material parsing, RAG, multi-agent collaboration, answer generation | User system, material permission decisions (backend gatekeeps) |
| Data | Persistence, vector retrieval, caching | Business logic |

---

## 4. Tech Stack Overview

| Layer | Technology | Notes |
|-------|-----------|-------|
| Frontend | React 18 + TypeScript + Vite + Zustand + React Router | SPA, component-based |
| Backend | Go 1.22 + Gin + GORM + JWT | Lightweight, high concurrency |
| Agent | Python 3.11 + Google ADK + A2A SDK | Multi-agent orchestration |
| Relational DB | PostgreSQL 16 | Business data |
| Vector | pgvector extension | Material embedding retrieval |
| Cache | Redis 7 | Session context, hot cache, rate limiting |

---

## 5. Frontend Design (React)

### 5.1 Architecture

- **Routing**: React Router, modularized by "Library / Learning Center / AI Companion / Profile".
- **State Management**: Zustand (global) + React Query (server cache).
- **UI**: Component library + design tokens for unified theming.
- **Real-time**: AI conversations use SSE streaming, with `/api/agent/chat` proxied from backend to Agent.

### 5.2 Self-Directed Learning Mode

- Material browsing/searching (by team, subject, chapter, tags).
- Reader + notes + annotations.
- Chapter exercises, with scores recorded by backend upon submission.
- Progress dashboard: study duration, completion rate, accuracy trends.

### 5.3 AI-Assisted Learning Mode

- **Smart Q&A**: Select text / entire page to ask, Agent answers based on RAG across multiple knowledge bases in the "visible team set", with citations (source team / material).
- **Study Planning**: Planner Agent generates plans by goal and deadline.
- **Smart Assessment**: Evaluator Agent generates quizzes and grades them.
- **Conversational Companion**: Multi-turn dialogue, context memory, browsable conversation history.

### 5.4 Key Pages

| Page | Description |
|------|-------------|
| Login / Register | Email or third-party login |
| My Teams | Private team + joined teacher teams + public library entry |
| Library | Materials listed by team; teachers can toggle `shared` visibility |
| Reader | Content + notes + one-click Q&A |
| AI Companion | Dialogue (streaming), session history, citation display |
| Learning Center | Plans, assessments, progress visualization |
| Admin Panel | Super admin: public library management |

---

## 6. Backend Design (Go + Gin)

### 6.1 Layering

```
handler (Gin) → service (business/RBAC/team) → repository (GORM) → PostgreSQL
                                  ↘ agent client (calls Agent service)
```

- **Middleware**: JWT authentication, RBAC validation, visible team computation, rate limiting, logging/tracing.
- **Agent Client**: Encapsulates HTTP calls to Agent, with timeout, retry, SSE stream forwarding; carries "user visible team set" in requests.

### 6.2 Permission Model (RBAC) & Team

The system has three roles, with permissions and material ownership bounded by "Team / Knowledge Base":

| Role | Team Behavior | Material Permissions |
|------|---------------|---------------------|
| `student` | Has an auto-generated **private team** (created upon registration); joins teacher teams via **class code** | Private materials visible only to self; can access `shared=true` materials in joined teacher teams; can access public library |
| `teacher` | Can create **study group teams** (generates class code `join_code`), approves student join requests | Only teacher can upload to own team; sets `shared` per material to control student visibility |
| `super_admin` | Manages system-level **public library** (single virtual team, `type='public'`) | Uploaded materials visible platform-wide |

**Upload Permission Rules (enforced at repository layer)**

| Operation | Allowed Roles | Notes |
|-----------|---------------|-------|
| Upload to private team | Only the team owner (student themselves) | Others cannot write |
| Upload to teacher team | Only the team's owner teacher | Students/other teachers cannot write |
| Upload to public team | Only super_admin | — |
| Set `shared` | Only effective for teacher team materials | `shared` on student private / public materials is ignored or rejected |

**Membership & Approval**
- `team_members` includes `status`: `pending` (awaiting approval) / `approved` (joined).
- Student uses teacher-provided `join_code` to call `POST /api/teams/:id/join` → enters `pending`; after teacher `approve`s → `approved`, then gains visibility into that team's materials.
- Public library (`type='public'`) **does not write `team_members` rows** — handled as a special case in "visible team set" computation to avoid member table bloat.

- Example permission points: `material:read`, `material:write`, `team:create`, `team:manage`, `team:approve`, `user:manage`, `agent:chat`.
- Data isolation: Learning records and conversation history are row-isolated by `user_id`; materials controlled by `team_id` + `shared` visibility.
- Key rule: **Each team is a knowledge base**; retrieval is strictly limited to the "user visible team set", and teacher teams only retrieve `shared=true` materials.

### 6.3 Team & Knowledge Base

- **Teacher Team (Study Group)**: Created by teacher (generates `join_code`), members are students; only teacher can upload materials; each material has a `shared` toggle — when on, `approved` students can see it; when off, only the teacher sees it (prep/draft).
- **Student Private Team**: Auto-generated per student at registration; materials uploaded by the student are visible only to themselves.
- **Public Library**: System-level team (`type='public'`) maintained by super admin; materials visible platform-wide; does not write to `team_members`, handled as special case in visibility computation.
- **Knowledge Base Essence**: A team is the logical container for materials and vectors. After upload, the Agent performs structural parsing (chunking, extracting chapters/knowledge points, generating embeddings) and writes into that team's `material_chunks`; RAG retrieval only fetches chunks within the "user visible team set", and teacher teams only fetch `shared=true`, naturally enforcing permission isolation.
- **Parse Automation**: After `POST /api/materials` succeeds, the backend **asynchronously** triggers the Parser task (file lands in object storage → enqueued → parsed → chunks written → `parse_status` updated); `parse_status` is `pending/parsing/done/failed`, shown by the frontend as progress.

### 6.4 Core Modules

| Module | Responsibility |
|--------|---------------|
| Auth | Registration/login, JWT issuance & refresh, roles |
| Team | Team creation, member management, visible team set computation |
| User | User profile, role, subscription status |
| Material | Material CRUD (belongs to team), `shared` visibility, triggers parsing |
| Learning | Learning records, exercise scores, progress aggregation |
| Agent Gateway | Proxies Agent requests, injects user context & "visible team set" |

### 6.5 API Design (REST Summary)

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/api/auth/login` | Login | No |
| POST | `/api/auth/register` | Register (default student, auto-create private team) | No |
| POST | `/api/auth/refresh` | Refresh token (httpOnly Cookie) | Yes |
| GET | `/api/teams` | My visible teams (private + joined + public) | Yes |
| POST | `/api/teams` | Teacher creates study group team (returns `join_code`) | teacher |
| POST | `/api/teams/:id/join` | Student requests to join via `join_code` (→ pending) | student |
| POST | `/api/teams/:id/members/:uid/approve` | Teacher approves member | teacher(owner) |
| GET | `/api/teams/:id/members` | Member & pending list | teacher(owner) |
| GET | `/api/teams/:id/materials` | Visible materials in team (auto-filters `shared`) | Yes |
| POST | `/api/materials` | Upload material to team (async triggers parsing) | Yes |
| GET | `/api/materials` | Material list (filtered by visible teams) | Yes |
| GET | `/api/materials/:id` | Material detail (includes `parse_status`) | Yes |
| PUT | `/api/materials/:id` | Update material / toggle `shared` (change triggers re-parse) | Yes |
| DELETE | `/api/materials/:id` | Delete material (cascade deletes chunks) | Yes |
| GET | `/api/learning/records` | My learning records | Yes |
| POST | `/api/learning/exercise` | Submit exercise | Yes |
| POST | `/api/agent/chat` | AI conversation (SSE streaming, retrieves visible teams + `shared`) | Yes |
| POST | `/api/agent/plan` | Generate study plan (writes `study_plans`) | Yes |
| POST | `/api/agent/quiz` | Generate quiz (writes `exercises`) | Yes |
| GET | `/api/agent/sessions` | My session list | Yes |

> All write endpoints are RBAC-constrained; `/api/agent/*` endpoints have the backend inject `user_id`, subscription tier, and "visible team set + `shared` filter predicate" into Agent requests. The Agent does not directly read permission tables — it only holds **read-only** credentials for `material_chunks` to perform retrieval.

---

## 7. Agent Design (Python + Google ADK + A2A)

### 7.1 Multi-Agent Roles

Uses an **Orchestrator + Specialized Agent** pattern, with agents collaborating via the **A2A protocol** (HTTP/JSON):

| Agent | Responsibility |
|-------|---------------|
| **Orchestrator** | Receives backend requests (with visible team set), classifies intent, decomposes sub-tasks, routes and aggregates |
| **Parser** | Structural material parsing: chunking, extracting chapters/knowledge points, generating embeddings, writing to the corresponding team |
| **Retriever** | Vector retrieval within the "visible team set", reranking, returning material chunks |
| **Tutor** | Q&A tutoring: generates accessible explanations based on retrieved chunks + conversation history, with citations (team/material/chapter) |
| **Planner** | Study planning: generates structured plans by goal/deadline/level |
| **Evaluator** | Assessment & grading: generates questions, scores, provides weak-point suggestions |
| **Memory** | Maintains user long-term profile & preferences (Redis + PostgreSQL) |

### 7.2 A2A Interaction Example (Q&A)

```
Backend ──POST /agent/chat (visible team set)──▶ Orchestrator
Orchestrator ──A2A task──▶ Retriever   (returns top-k within that team set only)
Orchestrator ──A2A task──▶ Tutor       (chunks + question + history → answer)
Orchestrator ──SSE stream──▶ Backend ──▶ Frontend
```

### 7.3 RAG Flow

1. Question (+ current material context) → Embedding.
2. Approximate search within the "visible team set" in `material_chunks`.
3. Rerank → assemble into prompt.
4. LLM generates answer, outputs citations (team / material / chapter).
5. New Q&A written to conversation history (Redis short-term + PostgreSQL long-term).

### 7.4 Context & Memory

- **Short-term Context**: Current conversation turns stored in Redis (configurable TTL).
- **Long-term Memory**: User profile, weak points, preferences stored in PostgreSQL.
- **Security**: Agent only holds read-only credentials for `material_chunks`; the retrieval predicate (visible team set + teacher team `shared=true` only) is computed by the backend and passed down — the Agent cannot directly access the full database or permission tables.

### 7.5 Material Structural Parsing & Team Knowledge Base

- Any material (teacher team / student private team / public library) upon upload triggers the backend to **asynchronously** invoke the Agent's **Parser task**: file lands in object storage → chunking → structure extraction → embedding generation → write to that team's `material_chunks` (updating `parse_status`).
- At retrieval time, the backend computes the **effective retrieval predicate** based on user identity: `team_id IN (visible set) AND (teams.type <> 'teacher' OR materials.shared = true)`, and vector retrieval is performed only within that predicate — teacher team drafts (`shared=false`) can never be recalled by students.
- Student private team materials are also parsed into their private knowledge base, enabling AI Q&A to draw from "their own materials."

---

## 8. Data Layer Design

### 8.1 PostgreSQL Core Tables (DDL Summary)

```sql
-- Users & Roles
CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  email         VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  display_name  VARCHAR(100),
  role          VARCHAR(20) NOT NULL DEFAULT 'student', -- student/teacher/super_admin
  subscription  VARCHAR(20) DEFAULT 'free',
  created_at    TIMESTAMPTZ DEFAULT now()
);

-- Teams / Knowledge Bases
CREATE TABLE teams (
  id          BIGSERIAL PRIMARY KEY,
  name        VARCHAR(200) NOT NULL,
  type        VARCHAR(20) NOT NULL,   -- private(student private) / teacher(teacher group) / public(public library)
  join_code   VARCHAR(20) UNIQUE,     -- only for teacher teams; students join via this code
  owner_id    BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ DEFAULT now()
);
-- Public library is a system-level single virtual team (type='public'), maintained by super admin,
-- does not write to team_members

CREATE TABLE team_members (
  team_id     BIGINT REFERENCES teams(id),
  user_id     BIGINT REFERENCES users(id),
  role        VARCHAR(20) DEFAULT 'member',  -- member / co_teacher (owner tracked via teams.owner_id)
  status      VARCHAR(20) DEFAULT 'approved',-- pending(awaiting approval) / approved(joined)
  joined_at   TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

-- Learning Materials (belong to a team / knowledge base)
CREATE TABLE materials (
  id          BIGSERIAL PRIMARY KEY,
  team_id     BIGINT REFERENCES teams(id),  -- owning team / knowledge base
  title       VARCHAR(300) NOT NULL,
  subject     VARCHAR(100),
  chapter     VARCHAR(100),
  tags        TEXT[],
  content     TEXT,                         -- body / Markdown (backfilled after parsing)
  file_type   VARCHAR(20),                  -- pdf / pptx / docx / md / image ...
  storage_key VARCHAR(512),                 -- object storage key (MinIO/S3)
  parse_status VARCHAR(20) DEFAULT 'pending', -- pending/parsing/done/failed
  parse_error VARCHAR(512),
  shared      BOOLEAN DEFAULT false,        -- only effective for teacher teams: visible to team student members
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

-- Quiz Questions & Attempts (Evaluator)
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

-- Study Plans (Planner)
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

-- User Long-term Profile (Memory Agent)
CREATE TABLE user_profiles (
  user_id     BIGINT PRIMARY KEY REFERENCES users(id),
  weak_points TEXT[],  -- weak knowledge points
  preferences JSONB,
  updated_at  TIMESTAMPTZ DEFAULT now()
);

-- Token Usage (cost attribution / subscription quota)
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

### 8.2 pgvector Vector Table (Team-Isolated)

```sql
CREATE EXTENSION IF NOT EXISTS vector;
-- Embedding model: uses Google ADK-compatible text-embedding model.
-- Dimension <DIM> is injected via environment variable EMBEDDING_DIM; must be consistent across the entire database.

CREATE TABLE material_chunks (
  id          BIGSERIAL PRIMARY KEY,
  team_id     BIGINT REFERENCES teams(id),    -- knowledge base ownership; filtered by visible team at retrieval
  material_id BIGINT REFERENCES materials(id),
  chunk_idx   INT,
  content     TEXT,
  embedding   vector(<DIM>)                    -- dimension determined by embedding model (e.g. Gemini 768 / 3072), injected via config
);
CREATE INDEX idx_chunk_team ON material_chunks(team_id);
CREATE INDEX idx_chunk_vec ON material_chunks
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
-- Retrieval predicate (backend passes down): team_id IN (visible set) AND (teams.type <> 'teacher' OR materials.shared = true)
```

### 8.3 Redis Caching Strategy

| Key Pattern | Content | TTL |
|-------------|---------|-----|
| `session:{session_id}` | Current conversation context (short-term memory) | 30 min idle |
| `team:visible:{user_id}` | User visible team set (accelerates auth/retrieval) | 5 min |
| `cache:material:{id}` | Hot material content | 10 min |
| `ratelimit:agent:{user_id}` | Conversation rate limit counter | 1 min |
| `user:profile:{user_id}` | User profile snapshot | 5 min |

> **Cache Invalidation**: When `material.shared` is toggled, or `team_members` are added/removed/status changed, actively delete `team:visible:{user_id}` and `user:profile:{user_id}` to prevent students from seeing drafts or missing newly public materials within the 5-minute window.

---

## 9. Security Design

- **Authentication**: JWT (access + refresh); refresh token uses **httpOnly + Secure Cookie** (preferred over localStorage, XSS-resistant); SSE streaming conversations pass access token via handshake first frame or query parameter (since `EventSource` cannot set custom headers).
- **Authorization**: RBAC + team visibility + row-level isolation; material visibility enforced at repository layer, with teacher teams only returning `shared=true`.
- **Agent Boundary**: Backend passes "visible team set + `shared` filter predicate" to Agent; Agent holds only **read-only** DB role for `material_chunks`, cannot directly access permission tables or the full database.
- **Transport**: Site-wide HTTPS; SSE/WebSocket also authenticated.
- **Content Safety**: K12 scenarios apply content moderation gateway (keyword + model guardrails) on both user input and model output to prevent inappropriate content.
- **Rate Limiting**: Agent conversations rate-limited per user.
- **Logging**: No plaintext passwords or tokens logged; conversation logs sanitized.

---

## 10. Deployment Architecture (Draft)

```
docker-compose:
  postgres  (16 + pgvector)
  redis     (7)
  backend   (Go container)
  agent     (Python container)
  frontend  (static build → Nginx / CDN)
```

- Frontend static assets served by Nginx, API proxied to backend.
- Backend and Agent communicate internally; Agent not exposed to public internet.
- Optional K8s: backend/agent independently HPA-scalable.

---

## 11. Observability

- Structured logging (JSON) collected centrally.
- Monitoring: QPS, P95 latency, Agent success rate, token consumption, parse task queue.
- Tracing: frontend → backend → Agent → pgvector linked via trace ID.
- Alerting: Agent error rate, DB connection pool, Redis memory thresholds.

---

## 12. Extensibility & Future Evolution

- Multi-tenancy (school/institution) isolation, teams can belong to tenants.
- Agent capability marketplace (pluggable new Agents).
- Cross-team retrieval authorization (e.g. student authorizes companion to read multiple knowledge bases).
- Assessment feedback loop: use learning outcomes to improve Planner/Evaluator.
- Cost optimization: cache hit rate, model routing.
