# Agent 服务（Python + FastAPI）

> 智能学伴系统 AI 层——多智能体协作（解析 / 检索 / 答疑 / 规划 / 测评），基于「团队知识库」RAG 提供个性化辅助。

[![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python)](https://www.python.org/)
[![FastAPI](https://img.shields.io/badge/FastAPI-0.111-009688?logo=fastapi)](https://fastapi.tiangolo.com/)
[![pgvector](https://img.shields.io/badge/pgvector-0.2-4169E1)](https://github.com/pgvector/pgvector)

---

## 架构概览 / Architecture

```
后端 repository ──可见性过滤+向量检索──▶ 已授权 chunks ──POST /chat──▶ FastAPI
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    ▼                       ▼                       ▼
              Embed API              Tutor/Planner/Evaluator      Parser
          (仅生成查询向量)           (LLM 生成 / Mock 降级)     (切分→嵌入→写库)
                    │                       │                       │
                    └───────────────────────┼───────────────────────┘
                                            ▼
                                   PostgreSQL + pgvector
                                   (Parser 最小读写凭证)
```

**安全边界（铁律）**：资料可见性谓词与 pgvector 检索只存在于 Backend repository；Agent 只生成 query embedding、解析资料并消费后端传入的已授权 chunks，不查询权限表，也不提供 `/retrieve`。

---

## 技术栈 / Tech Stack

| 组件 | 版本 / 库 | 说明 |
|------|-----------|------|
| 语言 | Python 3.11+ | — |
| Web 框架 | FastAPI 0.111 + Uvicorn | REST + SSE 流式 |
| 数据库 | psycopg2 + pgvector | PostgreSQL 向量检索 |
| Embedding | DashScope text-embedding-v4（1024 维） | OpenAI 兼容接口；本地确定性哈希兜底 |
| LLM | DeepSeek V3（deepseek-chat） | OpenAI 兼容 Chat Completions |
| 校验 | pydantic v2 | 请求/响应模型 |
| HTTP | httpx | 外部 API 调用（强制 timeout） |
| 质量 | pytest + ruff | 测试 + lint |

### 降级策略 / Fallback

| 层 | 生产模式 | 降级模式（无 API Key） |
|----|----------|----------------------|
| Embedding | DashScope text-embedding-v4（1024 维） | 本地确定性哈希嵌入（hashing trick，离线可用） |
| LLM | DeepSeek V3 via OpenAI 兼容接口 | MockLLM：基于召回片段抽取式组织（不调外部模型） |

> 设置 `LLM_API_KEY` 和 `EMBEDDING_API_KEY` 后自动切换为真实模型；不设置则全链路仍可本地跑通（呼应 R6 兜底）。

---

## 快速开始 / Quick Start

### 前置依赖

- Python 3.11+
- PostgreSQL 16（启用 `pgvector` 扩展，数据库迁移已执行）
- 后端服务已初始化数据库（`material_chunks` 表存在）

### 安装与运行

```bash
cd agent

# 1. 配置环境变量
cp .env.example .env
# 编辑 .env：填入 LLM_API_KEY（DeepSeek）和 EMBEDDING_API_KEY（DashScope）
# 不填则自动使用本地 Mock 降级（全链路仍可跑通）
# 生产的 learning_parser 账号由仓库根目录 make provision-parser 创建/授权。

# 2. 安装依赖
pip install -r requirements.txt

# 3. 启动
python main.py
# 默认监听 :8000，启动时自动断言 embedding 维度一致性（R1）
```

---

## 目录结构 / Project Structure

```
agent/
├── main.py                    # FastAPI 入口：路由注册 + SSE 流式 + 启动维度断言
├── db.py                      # 数据库连接 + pgvector 适配 + 配置管理（Settings）
├── embed.py                   # Embedding 生成器（DashScope / 本地哈希降级）
├── llm.py                     # LLM 可插拔生成层（DeepSeek / MockLLM 降级）
├── rag.py                     # 解析与生成编排：parse / run_chat / run_plan / run_quiz
├── schemas.py                 # Pydantic 请求/响应模型
├── conftest.py                # pytest 配置与 fixtures
├── pytest.ini                 # pytest 配置
├── ruff.toml                  # ruff lint + format 配置
├── requirements.txt           # Python 依赖
├── Dockerfile                 # 容器构建
├── .env.example               # 环境变量模板
└── tests/
    └── test_retrieve.py       # Agent 权限面 + 解析并发幂等测试
```

---

## 环境变量 / Environment Variables

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `LLM_API_KEY` | DeepSeek API 密钥 | —（**不填则降级 MockLLM**） |
| `LLM_BASE_URL` | LLM API 端点 | `https://api.deepseek.com/v1` |
| `LLM_MODEL` | 模型名称 | `deepseek-chat` |
| `PG_DSN` | PostgreSQL 连接串（需读写权限） | `postgres://postgres:postgres@localhost:5432/learning_buddy` |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `EMBEDDING_PROVIDER` | Embedding 提供方 | `openai`（兼容模式）/ 空则为本地降级 |
| `EMBEDDING_API_KEY` | DashScope API 密钥 | —（**不填则降级本地哈希嵌入**） |
| `EMBEDDING_BASE_URL` | Embedding API 端点 | 阿里云百炼 DashScope 兼容端点 |
| `EMBEDDING_MODEL` | Embedding 模型名称 | `text-embedding-v4` |
| `EMBEDDING_DIM` | 向量维度（全库统一） | `1024` |
| `AGENT_SHARED_SECRET` | Backend→Agent 服务认证共享密钥（必填） | — |
| `RETRIEVER_TIMEOUT_S` | 答疑查询 Embedding 超时预算（秒） | `0.8` |
| `PARSER_EMBEDDING_TIMEOUT_S` | 资料解析单个 chunk 的 Embedding 超时（秒） | `30` |
| `TUTOR_TIMEOUT_S` | 答疑 Tutor 单跳超时预算（秒） | `30` |
| `PORT` | HTTP 监听端口 | `8000` |

---

## API 端点 / API Endpoints

除 `/health` 外，所有端点仅由后端内部调用，必须携带 `X-Agent-Token: <AGENT_SHARED_SECRET>`；密钥缺失或错误返回 HTTP 401。生产部署不向宿主机或公网映射 Agent 端口。

### 健康检查

**`GET /health`**
```
→ 200 {"status": "ok"}
→ 200 {"status": "db_down"}   # 数据库不可达
```

### 资料解析

**`POST /parse`** — 资料结构化解析入库

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `material_id` | int | ✅ | 资料 ID |
| `content` | str | ✅ | 资料正文 / Markdown |
| `file_type` | str | | 文件类型（`pdf` / `md` / `txt` 等），默认 `txt` |
| `storage_key` | str | | 对象存储键 |

```json
// → 200
{ "material_id": 1, "chunks": 12, "status": "done" }
```

**幂等**：同一资料先获取 PostgreSQL 事务 advisory lock，再校验 Backend 下发的 `parse_generation` 与 `parse_status`，仅当前代次可替换 chunks；数据库唯一索引 `(material_id, chunk_idx)` 提供第二道防线。`parse_status` 仅由 Backend 状态机更新（R3）。

### 查询向量化

**`POST /embed`** — 为 Backend repository 检索生成查询向量

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `text` | str | ✅ | 待向量化文本 |

```json
// → 200
{
  "embedding": [0.01, -0.02]
}
```

**安全**：此接口不接受用户、team、shared 或 material ID；所有可见性判断和 top-k 查询都在 Backend repository 完成。

### AI 对话（SSE 流式）

**`POST /chat`** — 答疑 / 规划 / 测评统一入口（SSE 流式响应）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `question` | str | ✅ | 用户问题 / 目标 / 主题 |
| `session_id` | str | | 会话 ID |
| `history` | object[] | | 对话历史 `[{role, content}]` |
| `chunks` | object[] | | Backend repository 已过滤的资料片段 |
| `service` | str | | `chat`（答疑）/ `plan`（规划）/ `quiz`（测评），默认 `chat` |
| `deadline` | str | | 计划截止日期 |
| `goal` | str | | 计划目标 |
| `topic` | str | | 测评主题 |
| `count` | int | | 出题数量，默认 3 |

**SSE 事件流：**
```
data: {"type":"citations","items":[{team_id,material_id,chapter,chunk_idx,snippet}]}
data: {"type":"token","text":"关于"}
data: {"type":"token","text":"这个问题"}
...
data: {"type":"done","citations":[...]}
```

### 学习计划

**`POST /plan`** — 生成学习计划

```json
// Request
{ "goal": "两周后考教资", "deadline": "2026-07-26", "chunks": [] }

// → 200
{ "title": "学习计划：两周后考教资", "goal": "...", "deadline": "2026-07-26",
  "items": [{"date": "D1", "task": "预习核心概念", "done": false}, ...] }
```

### 智能测评

**`POST /quiz`** — 生成测评题目

```json
// Request
{ "topic": "牛顿力学", "count": 5, "chunks": [] }

// → 200
[
  { "question": "关于「牛顿力学」，下列说法正确的是？",
    "options": ["A选项", "B选项", "C选项", "D选项"],
    "answer_key": "A", "difficulty": "medium" },
  ...
]
```

---

## 安全边界 / Security

| 维度 | 约束 |
|------|------|
| 数据库凭证 | Parser 仅需读取资料归属、更新正文并读写 chunks；不访问 `users` / `team_members` / 认证数据 |
| 权限谓词 | **不自行拼装**——Backend repository 完成可见性过滤和向量检索，只下发 chunks |
| 参数化查询 | 所有 SQL 使用 `%s` 占位符 + psycopg2 参数绑定，无字符串拼接 |
| 启动断言 | `assert_embedding_dim()` 校验配置与库表的向量维度一致，不一致则拒绝启动（R1） |
| 服务认证 | 启动强制要求 `AGENT_SHARED_SECRET`；除 `/health` 外统一校验 `X-Agent-Token`（R5） |
| 降级安全 | MockLLM 基于召回片段组织回答，不会编造不存在的内容 |
| 内部调用 | 生产仅暴露容器内网；本地 Compose 映射 8000 供 R5/E2E 反向验收 |

---

## 常用命令 / Commands

```bash
# 启动服务
python main.py

# 运行测试
pytest -v

# 静态检查 + 自动修复
ruff check .
ruff format .

# 运行单个测试文件
pytest tests/test_retrieve.py -v
```

---

## 设计要点 / Design Notes

1. **维度一致性（R1）**：启动时查询 `material_chunks.embedding` 列的 `atttypmod`，与配置的 `EMBEDDING_DIM` 比对，不一致则抛异常阻止启动。
2. **权限边界（R2）**：Backend repository 先应用 `VisibleMaterialsScope` 再执行 pgvector top-k；指定 `material_id` 也必须进入同一子查询。Agent 没有 `/retrieve` 与权限参数，只消费过滤后的 chunks。
3. **可靠且幂等的解析（R3）**：Backend 独占 `parse_status` 并为每次入队递增 `parse_generation`；Agent 用 advisory lock 串行化写入，并只接受当前代次且仍为 `parsing` 的请求。唯一索引继续阻止重复 chunk，调度器负责超时、指数退避、恢复、死信与告警。
4. **服务认证（R5）**：Backend 为所有 Agent 请求注入共享密钥，Agent 使用常量时间比较并默认拒绝无凭证直调；健康检查保持公开。
5. **超时与降级（R6）**：Backend 为 Embedding + repository 检索设置 800ms 总预算，失败时下发空 chunks；Agent 为 Tutor 设置 30s 预算并可降级到 MockLLM。
6. **可插拔设计**：`get_llm()` / `get_embedder()` 根据环境变量自动切换真实模型 / 降级实现，切换零代码改动。
