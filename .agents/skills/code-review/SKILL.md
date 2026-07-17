---
name: code-review
description: Review staged and unstaged code changes before committing — catches security issues, missing tests, broken contracts, and violations of the project engineering standards across all three stacks (Go backend, React frontend, Python agent). Triggers on 'review my changes', 'check before commit', 'pre-commit review', 'code review', or when the user says 'commit' / 'ready to commit'.
---

# Learning Buddy Pre-Commit Code Review

This skill reviews all pending changes against the project's engineering standards (`docs/engineering-standards.md`) and system design (`docs/system-design.md`) before every commit. It catches principle violations, security issues, and missing tests that automated linting won't find.

## When to Run

Before every commit. If the user says "commit" or "ready to commit", run this review first and present findings before proceeding with the commit.

## Review Process

### 1. Gather Changes

Collect the diff of what's about to be committed:

```bash
git diff HEAD
git status --short
```

Check for new untracked files that should be part of the change. If no changes exist, report "Nothing to review" and stop.

### 2. Identify Change Scope

From the diff, classify which areas were touched:

- **Backend / Go** — files in `backend/` (`*.go`)
  - `model` — `backend/internal/model/`
  - `repository` — `backend/internal/repository/`
  - `service` — `backend/internal/service/`
  - `handler` — `backend/internal/handler/`
  - `middleware` — `backend/internal/middleware/`
  - `migrations` — `backend/migrations/`
  - `config` — `backend/internal/config/`
- **Agent / Python** — files in `agent/` (`*.py`)
  - `llm.py`, `embed.py`, `rag.py` — core AI logic
  - `db.py` — database access
  - `schemas.py` — Pydantic models
  - `main.py` — entry point
  - `tests/` — agent tests
- **Frontend / React** — files in `frontend/src/` (`*.ts`, `*.tsx`, `*.js`, `*.jsx`)
  - Pages, components, hooks, state
- **Docs** — `docs/`, `README.md`
  - `docs/prd.md`, `docs/system-design.md`, `docs/engineering-standards.md`, `docs/database.md`
- **Infra / Config** — `docker-compose.yml`, `Makefile`, `.githooks/`, `.gitignore`

This scope determines which checks apply.

### 3. Principle Review

For each relevant principle, check the diff against specific criteria. Report only actual violations — don't flag things that don't apply.

#### I. Security: Permission in Repository Layer (RED LINE)
*Applies when: repository, service, handler, or agent code changed*

This is the **iron rule** from `engineering-standards.md §0`:
> "凡涉及「用户可见资料范围」的逻辑，**只能写在后端 repository 层**，Agent 与前端永远不直接拼权限谓词。"

- If permission/visibility logic (`team_id IN(...)`, `shared`, `WHERE` clauses filtering by team) appears in handler, service, agent, or frontend code → **HIGH violation**
- Permission predicates MUST be in `backend/internal/repository/` — specifically `permission_test.go` demonstrates the expected pattern
- If a new RAG path is added in `agent/rag.py`, does it receive pre-filtered data from the backend? Agent must never construct its own team-visibility SQL

#### II. RAG Permission Isolation
*Applies when: repository, agent/rag.py, or agent/db.py changed*

- If `shared=false` teacher materials could leak to students through any code path → **HIGH violation**
- Check: does the retrieval predicate always include `AND (teams.type <> 'teacher' OR materials.shared = true)` for student queries?
- If new vector search or retrieval code is added, is the visibility filter applied BEFORE results are returned?
- Are there **negative test cases** asserting students CANNOT see `shared=false` teacher materials?

#### III. Embedding Dimension Consistency
*Applies when: agent/embed.py, agent/db.py, backend config, or migrations changed*

- Per R1 in engineering standards: embedding dimension must be single-source-of-truth
- If `EMBEDDING_DIM` appears hardcoded in multiple places → **MEDIUM violation**
- Check: does the startup/init code assert dimension matches the database `vector` column?
- If embedding model changed, was the migration plan documented?

#### IV. Error Handling & Timeouts
*Applies when: any Go or Python code changed*

**Go (backend):**
- Errors must wrap context: `fmt.Errorf("list materials: %w", err)` — not bare `return err`
- Handler layer must not `panic`
- All external calls (DB, Agent HTTP, Redis) must have timeout/context cancellation
- No `db.First` / `db.Take` — use `Where + Limit(1) + Find` pattern

**Python (agent):**
- All HTTP calls via `httpx` must have explicit timeout + retry config (per R6)
- No blocking calls inside async functions
- Structured logging, no `print()` (per §2.1)

**Frontend:**
- Error boundaries in place for new components?
- API errors handled gracefully with user-facing messages?

#### V. Test Coverage
*Applies when: any code changed*

- For new Go functions/methods: corresponding test in `*_test.go`?
- For repository permission logic: **must** have tests (red line — ≥90% coverage for security paths per §2.2)
- For agent logic: tests in `agent/tests/`?
- For frontend: test coverage for new components/hooks?
- If behavior changed: existing tests updated?
- Note: `gofmt`/`golangci-lint`/`ruff`/`eslint` are enforced by pre-commit hook — focus on test **existence**, not format/lint

#### VI. API Contract & Documentation
*Applies when: handlers, routes, or response structures changed*

- If a new endpoint was added or modified, is `docs/system-design.md` updated (if the architecture changed)?
- Is `docs/prd.md` updated (if user-facing behavior changed)?
- If a new backend→Agent A2A contract changed: is the contract documented and are both sides aligned?
- Do responses follow the established JSON patterns (`{code, data/result, error}`)?

#### VII. Database Integrity
*Applies when: models, migrations, or queries changed*

- Are schema changes accompanied by versioned migrations in `backend/migrations/`?
- Are there potential N+1 query issues (loops with individual DB calls)?
- If new vector operations: is `pgvector` index (`ivfflat`/`hnsw`) considered?
- Are foreign keys explicitly defined?

#### VIII. Concurrency & Shared State
*Applies when: service or repository logic changes involving team membership, shared flag, or approval state*

- Per R7/R10: shared state mutations (member approval, `shared` flip) must have lock/transaction protection
- Cache invalidation for `team:visible:{user_id}` must be centralized in repository write methods
- Any race condition between approval and in-flight queries?

#### IX. Observability
*Applies when: handlers, services, agent, or error paths changed*

- Go: using `log/slog` structured logging, not `fmt.Println`/`println`
- Python: using structured logging, not `print()`
- Key paths (auth, RAG retrieval, agent calls) have trace/log coverage?
- Sensitive fields (tokens, passwords) are redacted in logs?

#### X. No Hardcoded Secrets
*Applies when: any file changed*

- Scan diff for hardcoded: passwords, API keys, tokens, JWT secrets, connection strings
- Check that secrets come from env vars or config, never inline
- If `.env.example` was changed: does a real `.env` need updating? (never commit `.env`)

### 4. Stack-Specific Checks

#### Go Backend (additional)
- Context is always passed through (timeout/cancel/trace) — never `context.Background()` in request path
- New handlers invoke auth middleware or explicit role check? (check `router.go` for route registration)
- If new route added to `router.go`: does it have `RequireRole(...)` middleware if not public?
- Repository methods use `sqlc`-style typed queries where possible (per §2.1)

#### Python Agent (additional)
- All I/O boundaries use `pydantic v2` models (`agent/schemas.py`)
- New RAG/retrieval logic tested with `agent/tests/`
- A2A endpoints have authentication? (per R5: agent-to-agent auth required)
- Timeout budget per hop: Retriever ≤800ms, total answer with degradation plan (per R6)

#### React Frontend (additional)
- `strict: true`, no `any` — use proper TypeScript types
- API responses validated with `zod`?
- Using `react-query` for server state?
- SSE uses `fetch + ReadableStream` with Authorization header, NOT `EventSource` (per R4 and §2.1)
- No token in URL query parameters

### 5. Engineering Standards Checklist

Cross-reference against `docs/engineering-standards.md §2.3`:

| # | Check | Applies To |
|---|-------|------------|
| C1 | Security: permission filtering only in repository layer? | Backend |
| C2 | Errors: all external calls have timeout/retry? No bare panic? | Backend, Agent |
| C3 | Concurrency: shared state has lock/transaction protection? | Backend |
| C4 | Observability: key paths have trace/metric/log? Timeout budgets? | All stacks |
| C5 | Testing: new logic has unit tests? Boundary/error branches covered? | All stacks |
| C6 | Performance: N+1 queries? Vector search has top-k limit? | Backend, Agent |
| C7 | Documentation: API/schema changes synced to docs/? | All stacks |

### 6. Risk Register Check

If changes touch any P0 risk area from `engineering-standards.md §1`, flag:

| Risk | Trigger | Check |
|------|---------|-------|
| R1 | `embed.py`, `db.py` changes | Embedding dimension consistent across services? |
| R2 | `rag.py`, repository changes | RAG permission predicate unbypassed? |
| R3 | Parse pipeline changes | Timeout + idempotent retry + dead-letter? |
| R4 | Auth/SSE changes | Token not in URL? Using httpOnly cookie or short-lived ticket? |
| R5 | Agent A2A changes | Agent-to-agent auth in place? |
| R6 | Agent orchestration | Timeout budget per hop? Degradation fallback? |
| R7 | `shared`/member approval | Cache invalidation centralized? |

### 7. Report

Present findings in this format:

```
## 🔍 Pre-Commit Review

### Scope
- Backend: [files changed]
- Agent: [files changed]
- Frontend: [files changed]
- Docs/Config: [files changed]

### Principle Violations
| # | Principle | Severity | Finding |
|---|-----------|----------|---------|
| 1 | I. Security (Repo Layer) | 🔴 HIGH | Permission predicate in handler.go:45 — must move to repository |
| 2 | IV. Error Handling | 🟡 MEDIUM | Bare `return err` without context wrapping in service.go:32 |

### Stack Checks
| Stack | Status | Notes |
|-------|--------|-------|
| Go backend | ⚠️ 2 issues | See violations above |
| Python agent | ✅ Clean | |
| React frontend | ✅ Clean | |

### Engineering Standards Checklist
| Check | Status | Notes |
|-------|--------|-------|
| C1. Security | ❌ | Permission logic not in repo layer |
| C2. Errors | ⚠️ | Missing timeout on agent HTTP call |
| C3. Concurrency | ✅ | |
| C4. Observability | ✅ | |
| C5. Testing | ❌ | No tests for new handler |
| C6. Performance | ✅ | |
| C7. Documentation | ⚠️ | New endpoint not in system-design.md |

### Risk Register
| Risk | Status | Notes |
|------|--------|-------|
| R2 (RAG isolation) | ⚠️ | New retrieval path — verify predicate applied |

### Verdict
- 🔴 BLOCK: [N] items must be fixed before commit
- 🟡 WARN: [N] items to consider (won't block)
- 🟢 PASS: Ready to commit
```

Severity levels:
- **🔴 HIGH** — Must fix before commit: security issues, permission in wrong layer, missing tests for critical paths, hardcoded secrets
- **🟡 MEDIUM** — Should fix: missing documentation, error wrapping, timeout configuration
- **🟢 LOW** — Nice to have: naming suggestions, minor refactors

### 8. After Review

- If **BLOCK** items exist: list each with the file and line, offer to help fix
- If only **WARN/LOW**: present findings, ask "Fix these or proceed with commit?"
- If **PASS**: say "✅ All clear. Ready to commit." and proceed with `git commit`
