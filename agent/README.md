# Agent 服务（Python + FastAPI）

> 智能学伴系统 AI 层——多智能体协作（解析 / 检索 / 答疑 / 规划 / 测评），基于「团队知识库」RAG 提供个性化辅助。

[![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python)](https://www.python.org/)
[![FastAPI](https://img.shields.io/badge/FastAPI-0.111-009688?logo=fastapi)](https://fastapi.tiangolo.com/)
[![pgvector](https://img.shields.io/badge/pgvector-0.2-4169E1)](https://github.com/pgvector/pgvector)

---

## 架构概览 / Architecture

```
后端(Go) ──POST /chat (可见 team 集合)──▶ FastAPI
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    ▼                       ▼                       ▼
              Retriever              Tutor/Planner/Evaluator      Parser
         (pgvector 向量检索)         (LLM 生成 / Mock 降级)     (切分→嵌入→写库)
                    │                       │                       │
                    └───────────────────────┼───────────────────────┘
                                            ▼
                                   PostgreSQL + pgvector
                                   (仅持 material_chunks 只读凭证)
```

**安全边界（铁律）**：Agent 仅持 `material_chunks` 的只读/解析写凭证；「可见 team 集合 + `shared` 过滤谓词」由后端 repository 计算后通过请求注入，Agent 不自行判定成员/权限。

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
├── rag.py                     # RAG 核心：retrieve / parse / run_chat / run_plan / run_quiz
├── schemas.py                 # Pydantic 请求/响应模型
├── conftest.py                # pytest 配置与 fixtures
├── pytest.ini                 # pytest 配置
├── ruff.toml                  # ruff lint + format 配置
├── requirements.txt           # Python 依赖
├── Dockerfile                 # 容器构建
├── .env.example               # 环境变量模板
└── tests/
    └── test_retrieve.py       # RAG 检索单测 + 集成测试
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
| `PORT` | HTTP 监听端口 | `8000` |

---

## API 端点 / API Endpoints

所有端点由后端内部调用（不直接暴露公网）。

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

**幂等**：重解析前先删旧 chunks，再写新 chunks（R3）。

### 向量检索

**`POST /retrieve`** — 在可见 team 集合内做向量检索

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | str | ✅ | 检索查询 |
| `visible_team_ids` | int[] | ✅ | 后端计算的可见 team 集合 |
| `only_shared_in_teacher` | bool | | teacher team 仅取 `shared=true`，默认 true |
| `top_k` | int | | 返回 top-k 片段，默认 5（上限 50） |

```json
// → 200
{
  "chunks": [
    { "team_id": 1, "material_id": 10, "chapter": "第一章", "chunk_idx": 0, "content": "..." }
  ]
}
```

**安全**：检索谓词使用参数化 SQL（非字符串拼接），且仅在后端授权的 team 集合内过滤 `shared`。

### AI 对话（SSE 流式）

**`POST /chat`** — 答疑 / 规划 / 测评统一入口（SSE 流式响应）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `question` | str | ✅ | 用户问题 / 目标 / 主题 |
| `session_id` | str | | 会话 ID |
| `history` | object[] | | 对话历史 `[{role, content}]` |
| `visible_team_ids` | int[] | | 可见 team 集合 |
| `top_k` | int | | 检索片段数，默认 5 |
| `service` | str | | `chat`（答疑）/ `plan`（规划）/ `quiz`（测评），默认 `chat` |
| `material_id` | int | | 限定在某资料内检索（测评用） |
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
{ "goal": "两周后考教资", "deadline": "2026-07-26", "visible_team_ids": [1, 2] }

// → 200
{ "title": "学习计划：两周后考教资", "goal": "...", "deadline": "2026-07-26",
  "items": [{"date": "D1", "task": "预习核心概念", "done": false}, ...] }
```

### 智能测评

**`POST /quiz`** — 生成测评题目

```json
// Request
{ "topic": "牛顿力学", "material_id": 1, "count": 5, "visible_team_ids": [1] }

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
| 数据库凭证 | Agent 仅持 `material_chunks` 的读/写权限，不访问 `users` / `team_members` / 密码哈希等 |
| 权限谓词 | **不自行拼装**——`team_id` 集合与 `shared` 过滤由后端计算后通过请求注入 |
| 参数化查询 | 所有 SQL 使用 `%s` 占位符 + psycopg2 参数绑定，无字符串拼接 |
| 启动断言 | `assert_embedding_dim()` 校验配置与库表的向量维度一致，不一致则拒绝启动（R1） |
| 降级安全 | MockLLM 基于召回片段组织回答，不会编造不存在的内容 |
| 内部调用 | 不暴露公网，由后端做调用方鉴权 |

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
2. **权限边界（R2）**：`retrieve()` 的检索 SQL 中 `AND (t.type <> 'teacher' OR m.shared = true)` 与后端 repository 的 `VisibleMaterialsScope` 谓词完全一致，但只能缩小范围、不可扩大——team 集合由后端授权。
3. **幂等解析（R3）**：`parse()` 先 `DELETE` 旧 chunks 再 `INSERT` 新 chunks，重复调用不产生重复数据。
4. **全链路兜底（R6）**：MockLLM + LocalEmbedder 保证即使没有任何 API Key，答疑/规划/测评全链路仍可本地跑通。
5. **可插拔设计**：`get_llm()` / `get_embedder()` 根据环境变量自动切换真实模型 / 降级实现，切换零代码改动。
