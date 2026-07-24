# AI Learning Companion · System Design Document

> Version: v0.6 · Status: Design Draft (Revised) · Last Updated: 2026-07-18
> Changelog: v0.6 adds optional material scoping to conversations so global and material-specific histories cannot be mixed, while keeping the Backend repository as the only visibility authority.

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
│  · Repository auth filter + pgvector top-k → authorized chunks│
└──────┬──────────────────────┬───────────────────┬───────────┘
       │                      │                   │
       ▼                      ▼                   ▼
┌──────────────┐     ┌────────────────┐   ┌──────────────────────┐
│ PostgreSQL   │     │   Redis 7      │   │  Agent Layer (Python)  │
│ + pgvector   │     │ Session/Hot/RL │   │  Google ADK + A2A      │
│ teams/mat/vec│     │               │   │  Orchestrator+Parser    │
└──────────────┘     └────────────────┘   │  +Tutor+Planner+Eval   │
                                          └──────────┬───────────┘
                                                     │ A2A (HTTP/JSON)
                                          ┌──────────▼───────────┐
                                          │ Parser writes current  │
                                          │ generation chunks; all │
                                          │ generation consumes    │
                                          │ authorized chunks only │
                                          └──────────────────────┘
```

### 3.1 Layer Responsibilities

| Layer | Responsibility | NOT Responsible For |
|-------|---------------|---------------------|
| Frontend | Interaction, state, rendering, calling backend APIs | Business logic, data storage, model inference |
| Backend | Authentication, Team/RBAC, Material CRUD, visibility filtering, pgvector retrieval, Agent proxy | Model inference |
| Agent | Intent understanding, material parsing, multi-agent collaboration, generation from authorized chunks | User system, permission decisions, vector retrieval |
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

- **Smart Q&A**: The Backend repository performs RAG inside the user's authorized scope, then the Agent answers only from the filtered chunks and returns source citations.
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

- **Middleware**: JWT authentication, RBAC validation, rate limiting, and logging/tracing. Material visibility and pgvector retrieval are centralized in the repository.
- **Agent Client**: Encapsulates HTTP calls to Agent, with timeout, retry, and SSE forwarding; generation requests contain only repository-filtered chunks, never permission predicates.

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
- Key rule: **Each team is a knowledge base**; reads are strictly limited to the "user visible team set". Owners may manage drafts in their own teacher teams; other users must be `approved` members and the material must have `shared=true`. Detail, team-list, and note endpoints reuse the same repository scope.

### 6.3 Team & Knowledge Base

- **Teacher Team (Study Group)**: Created by teacher (generates `join_code`), members are students; only teacher can upload materials; each material has a `shared` toggle — when on, `approved` students can see it; when off, only the teacher sees it (prep/draft).
- **Student Private Team**: Auto-generated per student at registration; materials uploaded by the student are visible only to themselves.
- **Public Library**: System-level team (`type='public'`) maintained by super admin; materials visible platform-wide; does not write to `team_members`, handled as special case in visibility computation.
- **Knowledge Base Essence**: A team is the logical container for materials and vectors. After upload, the Agent Parser structurally parses and embeds content, writing `material_chunks` through a least-privilege database role. For Q&A, planning, and quizzes, the Backend repository owns both visibility filtering and pgvector top-k; the Agent consumes only authorized chunks.
- **Parse Automation**: After `POST /api/materials` succeeds, the backend **asynchronously** triggers the Parser task (file lands in object storage → enqueued → parsed → chunks written → `parse_status` updated); `parse_status` is `pending/parsing/done/failed`, shown by the frontend as progress.

### 6.4 Core Modules

| Module | Responsibility |
|--------|---------------|
| Auth | Registration/login, JWT issuance & refresh, roles |
| Team | Team creation, member management, visible team set computation |
| User | User profile, role, subscription status |
| Material | Material CRUD (belongs to team), `shared` visibility, triggers parsing |
| Learning | Learning records, exercise scores, progress aggregation |
| Agent Gateway | Retrieves authorized chunks through the repository, proxies Agent generation, and forwards SSE |

### 6.5 API Design (REST Summary)

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/api/auth/login` | Login | No |
| POST | `/api/auth/register` | Register (default student, auto-create private team) | No |
| POST | `/api/auth/refresh` | Refresh token (httpOnly Cookie) | Yes |
| POST | `/api/auth/logout` | Revoke refresh token family and clear Cookie | Yes |
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
| POST | `/api/materials/:id/retry` | Retry a `failed` parse task (requires team write access) | Yes |
| DELETE | `/api/materials/:id` | Delete material (cascade deletes chunks) | Yes |
| GET | `/api/learning/records` | My learning records | Yes |
| POST | `/api/learning/exercise` | Submit exercise | Yes |
| POST | `/api/agent/chat` | AI conversation (Backend retrieves authorized chunks, Agent streams generation) | Yes |
| POST | `/api/agent/plan` | Generate study plan (writes `study_plans`) | Yes |
| POST | `/api/agent/quiz` | Generate quiz (writes `exercises`) | Yes |
| GET | `/api/agent/sessions` | My session list | Yes |
| GET | `/api/agent/sessions/:id` | Restore structured messages and citations from my session | Yes |
| PUT | `/api/agent/messages/:id/feedback` | Idempotently rate an assistant answer in my session | Yes |

> All write endpoints are RBAC-constrained. Backend repository applies the canonical visibility predicate and pgvector top-k before sending authorized chunks to Agent. Agent Parser holds only the minimum database privileges needed for parsing and cannot read user, membership, or authentication tables.

---

## 7. Agent Design (Python + Google ADK + A2A)

### 7.1 Multi-Agent Roles

Uses an **Orchestrator + Specialized Agent** pattern, with agents collaborating via the **A2A protocol** (HTTP/JSON):

| Agent | Responsibility |
|-------|---------------|
| **Orchestrator** | Receives backend requests with repository-filtered chunks, classifies intent, routes and aggregates |
| **Parser** | Structural material parsing: chunking, extracting chapters/knowledge points, generating embeddings, writing to the corresponding team |
| **Backend Retriever** | Applies the repository visibility scope before pgvector top-k and returns authorized chunks |
| **Tutor** | Q&A tutoring: generates accessible explanations based on retrieved chunks + conversation history, with citations (team/material/chapter) |
| **Planner** | Study planning: generates structured plans by goal/deadline/level |
| **Evaluator** | Assessment & grading: generates questions, scores, provides weak-point suggestions |
| **Memory** | Maintains user long-term profile & preferences (Redis + PostgreSQL) |

### 7.2 A2A Interaction Example (Q&A)

```
Backend repository ──visibility scope + pgvector top-k──▶ authorized chunks
Backend ──POST /agent/chat (chunks)──▶ Orchestrator
Orchestrator ──A2A task──▶ Tutor       (chunks + question + history → answer)
Orchestrator ──SSE stream──▶ Backend ──▶ Frontend
```

### 7.3 RAG Flow

1. Backend calls Agent `/embed` for the query embedding (within the 800ms retrieval budget).
2. Backend repository searches `material_chunks` only through `VisibleMaterialsScope`; optional `material_id` uses the same scope.
3. Rerank → assemble into prompt.
4. LLM generates answer, outputs citations (team / material / chapter).
5. New Q&A written to conversation history (Redis short-term + PostgreSQL long-term).

> **R6 timeouts and fallback**: Backend gives embedding plus repository retrieval an `800ms` total budget and falls back to empty chunks; Agent gives Tutor a `30s` budget and falls back to the local MockLLM. SSE returns `X-Trace-ID` for correlation.

### 7.4 Context & Memory

- **Short-term Context**: Current conversation turns stored in Redis (configurable TTL).
- **Long-term Memory**: User profile, weak points, preferences stored in PostgreSQL.
- **Security**: visibility predicates and vector SQL exist only in Backend repository. Agent exposes no `/retrieve` route and receives only authorized chunks; Parser credentials must not access user, membership, or authentication data.

### 7.5 Material Structural Parsing & Team Knowledge Base

- Backend repository is the sole owner of the durable `materials.parse_status` state machine and increments `parse_generation` for every requeue. Worker completion and Agent chunk replacement must match the current generation; Agent also serializes writes with `pg_advisory_xact_lock(material_id)` and requires the status to remain `parsing`. Late requests therefore cannot overwrite newer content or terminal state, while the unique `(material_id, chunk_idx)` index prevents duplicate chunks.
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
  parse_generation BIGINT DEFAULT 1,          -- incremented on requeue; rejects stale workers
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
  material_id BIGINT REFERENCES materials(id) ON DELETE CASCADE, -- NULL=global; otherwise fixed material scope
  title       VARCHAR(200),
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_as_user ON agent_sessions(user_id);

CREATE TABLE agent_messages (
  id          BIGSERIAL PRIMARY KEY,
  session_id  UUID REFERENCES agent_sessions(id) ON DELETE CASCADE,
  role        VARCHAR(20),
  content     TEXT,
  citations   JSONB,   -- [{team_id, material_id, chapter, chunk_idx}]
  created_at  TIMESTAMPTZ DEFAULT now()
);

-- Quiz Questions & Attempts (Evaluator)
CREATE TABLE exercises (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  material_id BIGINT REFERENCES materials(id),
  session_id  UUID REFERENCES agent_sessions(id) ON DELETE SET NULL,
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
-- Retrieval predicate (executed directly by Backend repository): team_id IN (visible set) AND (teams.type <> 'teacher' OR materials.shared = true)
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

> **Current implementation (R7)**: These Redis keys remain a target design. The Go backend does not currently cache visibility; every visibility query uses the repository and PostgreSQL as the source of truth, so shared toggles and member approvals take effect on the next query. Before enabling the cache, invalidation must be centralized in repository write methods and continue to pass the immediate-visibility integration tests.

---

## 9. Security Design

- **Authentication**: JWT (access + refresh). SSE streaming conversations must use `fetch + ReadableStream`; the access token is carried only in the `Authorization: Bearer` header. `EventSource` and URL query tokens are forbidden so credentials never enter access logs.
- **Authorization**: RBAC + team visibility + row-level isolation; material visibility enforced at repository layer, with teacher teams only returning `shared=true`.
- **Agent Boundary**: Backend repository performs authorization and vector retrieval, then passes only authorized chunks to Agent. Every business request must include `X-Agent-Token`; Agent compares it in constant time and rejects unauthenticated calls by default. Agent cannot access user, membership, or authentication data.
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
- Backend and Agent communicate only on the container network. Agent has no host port mapping and is not exposed publicly; both services receive the same `AGENT_SHARED_SECRET` from the runtime environment.
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
