"""数据库连接与 pgvector 适配（Agent 仅持 material_chunks 的只读/解析写凭证）。

安全边界（见 docs/engineering-standards.md §0 / R2 与 system-design.md §7.4）：
- 检索「可见 team 集合」由后端 repository 计算后通过请求注入，Agent 不自行判定成员/权限。
- 本模块只负责连接与向量适配；任何权限谓词都不在此拼装。
"""

from __future__ import annotations

import os
from collections.abc import Iterator
from contextlib import contextmanager

import psycopg2
from pgvector.psycopg2 import register_vector
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # 本地默认使用 postgres 超级用户，确保解析流程可写 material_chunks / materials。
    # 生产应改为「仅可读 material_chunks + 可写解析结果」的受限凭证。
    pg_dsn: str = "postgres://postgres:postgres@localhost:5432/learning_buddy"
    redis_addr: str = "localhost:6379"
    embedding_dim: int = 1024  # 全库必须一致（engineering-standards R1）；真实 embedding 为 1024 维
    embedding_provider: str = "openai"  # local | openai（接入真实嵌入走 openai 兼容）

    # 生成（LLM / 答疑 / 计划 / 测评）—— DeepSeek
    llm_api_key: str = ""
    llm_base_url: str = "https://api.deepseek.com/v1"
    llm_model: str = "deepseek-chat"  # DeepSeek V3（通用/快速）；如需强推理改 deepseek-reasoner

    # 嵌入（Embedding）—— 阿里云百炼 DashScope text-embedding-v4（1024 维）
    embedding_api_key: str = ""
    embedding_base_url: str = (
        "https://llm-h85dzp0s5asc2v6i.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
    )
    embedding_model: str = "text-embedding-v4"

    port: int = 8000


settings = Settings()


@contextmanager
def get_conn() -> Iterator[psycopg2.extensions.connection]:
    """每个请求一条连接；注册 pgvector 适配器。"""
    conn = psycopg2.connect(settings.pg_dsn, connect_timeout=5)
    try:
        register_vector(conn)
        yield conn
        conn.commit()
    except Exception:
        conn.rollback()
        raise
    finally:
        conn.close()


def health_ok() -> bool:
    try:
        with get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1")
                return cur.fetchone() is not None
    except Exception:
        return False


def assert_embedding_dim() -> None:
    """启动时断言配置的 embedding 维度与数据库中 material_chunks.embedding 列一致。

    若不一致直接抛出异常阻止启动（engineering-standards R1：全库维度必须统一）。
    """
    configured = embedding_dim()
    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT atttypmod FROM pg_attribute "
                "WHERE attrelid = 'material_chunks'::regclass AND attname = 'embedding'"
            )
            row = cur.fetchone()
            if row is None:
                raise RuntimeError(
                    "material_chunks.embedding 列不存在，请确认已执行迁移"
                )
            # pgvector 列 typmod: vector(N) → atttypmod = N + 4
            db_dim = row[0] - 4 if row[0] > 4 else row[0]
            if db_dim != configured:
                raise RuntimeError(
                    f"embedding 维度不一致：配置为 {configured}，"
                    f"但 material_chunks.embedding 列为 vector({db_dim})。"
                    f"请统一 EMBEDDING_DIM 或重新执行迁移。"
                )


def embedding_dim() -> int:
    return int(os.environ.get("EMBEDDING_DIM", settings.embedding_dim))
