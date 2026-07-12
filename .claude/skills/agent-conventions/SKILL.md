---
name: agent-conventions
description: Python Agent 服务（learning_buddy/agent）开发规范——模块职责、asyncio 纪律、Pydantic 模型、RAG 安全边界、向量维度一致性、日志、工具链。编写或评审 agent/ 代码时自动遵循。
---

# Agent 开发规范（Python / FastAPI / Google ADK / A2A）

适用目录：`agent/`。技术栈：Python 3.11+ + FastAPI + psycopg2 + pgvector + httpx + Pydantic v2。

## 0. 架构铁律

### 0.1 安全边界（最高优先级）

> **Agent 仅持 material_chunks 的检索/解析写凭证；「可见 team 集合」由后端 repository 计算后通过请求下发，Agent 不自行判定成员/权限。**

- 检索谓词 `team_id IN(可见集) AND (type<>'teacher' OR shared)` 中的 `visible_team_ids` 来自请求参数（后端下发），Agent 只在该集合内做 refinement。
- `only_shared_in_teacher` 标识由后端根据**调用者角色**注入（后端 repository 是权限唯一真源），Agent 无需也不应感知学生/老师角色。
- 违反此条视为严重缺陷（engineering-standards §0 / R2）。

### 0.2 模块职责

```
agent/
├── main.py      # FastAPI 入口（路由 + SSE），禁止写业务逻辑
├── db.py        # 数据库连接 + pgvector 适配 + 配置（Settings）
├── embed.py     # 文本向量化（local hashing / OpenAI 兼容）
├── rag.py       # RAG 检索 + 解析 + 编排（Retriever/Parser/Tutor）
├── llm.py       # LLM 调用封装（DeepSeek 生成）
├── schemas.py   # 请求/响应 Pydantic 模型（与后端契约一致）
├── conftest.py  # pytest fixtures
└── tests/       # 测试
```

- `main.py` 不做业务逻辑，只做路由 + 参数解析 + 调用模块。
- `db.py` 不做权限过滤，只负责连接。
- `rag.py` 不做权限判定，只消费下发的 `visible_team_ids`。

### 0.3 Embedding 维度一致性（R1）

- **全库维度必须统一**（engineering-standards R1）：配置（`Settings.embedding_dim`）、代码默认值、`.env.example`、pgvector 列定义、实际模型输出必须一致。
- 启动时 `assert_embedding_dim()` 校验配置与 `material_chunks.embedding` 列的 `vector(N)` 维度。
- 维度变更 = 迁移脚本 + 全库重建索引 + `.env.example` 同步。

## 1. 编码规范

### 1.1 格式化与 Lint（ruff）

`ruff.toml` 配置：
```toml
line-length = 100
target-version = "py311"

[format]
quote-style = "double"

[lint]
select = ["E", "F", "I", "B", "UP", "W"]
ignore = ["E501"]
```

- 行宽 100，双引号，isort 自动排序导入。
- W 系列用于警示（如 `W291` 行尾空格），保持文件整洁。
- E501（行长）在 ruff 层面忽略，实际由 formatter 的 `line-length=100` 处理。

### 1.2 类型检查（mypy --strict）

- 所有 I/O 边界（函数签名、API 路由）必须有完整类型注解。
- `mypy --strict` 零容忍。
- 禁止在同步函数内写阻塞调用。

### 1.3 导入顺序

```python
# 1. 标准库
from __future__ import annotations
import os

# 2. 第三方
import httpx
from fastapi import FastAPI
from pydantic import BaseModel

# 3. 本地
from db import settings, get_conn
from embed import get_embedder
```

### 1.4 文档字符串

```python
def retrieve(
    query: str,
    visible_team_ids: list[int] | None,
    only_shared_in_teacher: bool = True,
    top_k: int = 5,
) -> list[ChunkView]:
    """在后端授权的可见 team 集合内做向量检索（余弦最近邻）。

    Args:
        visible_team_ids: 后端 repository 计算下发的可见 team 集合。
        only_shared_in_teacher: 后端注入的 shared 过滤标志（学生=true）。
    """
```

- 每个公开函数必须有 docstring 说明意图。
- 安全相关参数必须注释来源（谁负责计算/注入）。

## 2. Pydantic 模型（I/O 边界）

```python
from pydantic import BaseModel

class RetrieveRequest(BaseModel):
    query: str
    visible_team_ids: list[int]
    only_shared_in_teacher: bool = True
    top_k: int = 5
```

- 所有 API 请求/响应使用 `pydantic v2` 模型。
- 模型定义在 `schemas.py` 统一管理（与后端 `service/agent.go` 的 HTTP 契约保持一致）。
- 字段类型显式，默认值明确。
- **不使用** `Any` 类型（mypy 将拒绝）。

## 3. 异步规范（asyncio）

```python
# ✅ 正确：async 函数 + httpx
async def call_llm(prompt: str) -> str:
    async with httpx.AsyncClient(timeout=800) as client:
        resp = await client.post(url, json={"prompt": prompt})
        return resp.json()["answer"]

# ❌ 错误：同步函数里阻塞调用
def call_llm(prompt: str) -> str:
    resp = requests.post(url, json={"prompt": prompt})  # 阻塞事件循环
    return resp.json()["answer"]
```

- **不在同步函数内阻塞** — 同步函数（如 `do_retrieve`）若调 I/O 需改为 async。
- MVP 阶段同步 DB 查询（`psycopg2`）可直接使用（FastAPI 会在线程池运行同步路由），但不要在 async 函数里调用同步 DB 操作。

## 4. HTTP 客户端（httpx）

```python
import httpx

# ✅ 正确：强制 timeout + 有限重试
async with httpx.AsyncClient(timeout=httpx.Timeout(30.0)) as client:
    try:
        resp = await client.get(url)
    except httpx.TimeoutException:
        # 降级为无 RAG 回答（engineering-standards R6）
        return fallback_answer()

# ❌ 错误：无 timeout
resp = requests.post(url, json=data)
```

- 所有外部 HTTP 调用必须设 timeout（engineering-standards R6：每跳超时预算，如 Retriever ≤800ms）。
- Retriever 失败降级为无 RAG 直接回答（不断联）。
- 使用 `httpx.AsyncClient` 复用连接池。

## 5. 日志

```python
import logging

logger = logging.getLogger("agent")

logger.info("retrieve", extra={"query_len": len(query), "visible_teams": len(visible)})
logger.error("embedding failed", exc_info=True)
```

- 结构化日志，避免 `print()`。
- 关键路径：检索、解析、LLM 调用、错误降级。
- 生产环境日志级别 `INFO`，开发环境 `DEBUG`。

## 6. 数据库（psycopg2 + pgvector）

```python
from db import get_conn  # 不直接创建连接

with get_conn() as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT ...", params)
```

- 统一使用 `get_conn()` 上下文管理器（自动 commit/rollback/close + pgvector 适配）。
- SQL 参数化查询（`%s` 占位），禁止字符串拼接。
- 向量检索使用 `<=>` 余弦距离算子。

## 7. 测试

```python
# agent/tests/test_retrieve.py
def test_retrieve_filters_by_visible_teams():
    """检索仅返回可见 team 内的 chunks"""
    chunks = retrieve("keyword", visible_team_ids=[1, 2])
    assert all(c.team_id in {1, 2} for c in chunks)

def test_retrieve_excludes_unshared_teacher_material():
    """当 only_shared_in_teacher=True 时排除 shared=false 的 teacher 资料"""
    # R2 反向测试：断言不包含非共享草稿
```

- RAG 权限测试**必须带反向用例**（R2）：断言学生视角不含 `shared=false` 的 teacher 资料。
- 测试文件在 `tests/` 目录下按模块命名（`test_retrieve.py`, `test_embed.py`）。
- 使用 `pytest` + `conftest.py` 管理 fixtures。

## 8. 工具链

| 工具 | 命令 | 用途 |
|------|------|------|
| `ruff format .` | 格式化 | 自动修复格式 |
| `ruff check .` | Lint | 代码质量检查 |
| `mypy --strict .` | 类型检查 | 类型零容忍 |
| `pytest` | 单测 | 测试收集与运行 |
| `bandit` | 安全扫描 | CI 使用（High/Critical 阻断） |

`.pre-commit` hook 自动执行：
```bash
ruff format .
ruff check .
```

## 9. 配置管理（Settings）

```python
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    pg_dsn: str = "postgres://..."
    redis_addr: str = "localhost:6379"
    embedding_dim: int = 1024  # 全库必须一致（R1）
    embedding_provider: str = "openai"  # local | openai
    llm_api_key: str = ""
    llm_base_url: str = "https://api.deepseek.com/v1"
    llm_model: str = "deepseek-chat"
    embedding_api_key: str = ""
    embedding_base_url: str = "https://llm-h85dzp0s5asc2v6i.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
    embedding_model: str = "text-embedding-v4"
    port: int = 8000

settings = Settings()
```

- 配置集中到 `db.py:Settings`（`pydantic-settings BaseSettings` 自动从 `.env` 和 OS 环境变量读取）。
- `extra="ignore"`：忽略未定义的环境变量，避免意外参数。
- 任何新配置项必须在 `agent/.env.example` 中同步添加。

## 10. 禁止清单

- ❌ Agent 自行拼权限谓词或判定角色（铁律）
- ❌ 同步函数内阻塞调用（`requests.get` in sync）
- ❌ HTTP 无 timeout（`httpx` 必须带 timeout）
- ❌ 用 `print()` 替代结构化日志
- ❌ 字符串拼接 SQL（用 `%s` 参数化）
- ❌ `EMBEDDING_DIM` 硬编码多处或不一致
- ❌ I/O 边界无 Pydantic 模型
- ❌ 提交 `ruff check` error / `mypy --strict` error
- ❌ 改动 `shared` 过滤逻辑不带反向测试
