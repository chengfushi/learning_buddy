"""数据库连接与 pgvector 适配（Agent Parser 仅持解析所需的最小读写凭证）。

安全边界（见 docs/engineering-standards.md §0 / R2 与 system-design.md §7.4）：
- 检索可见性与 pgvector top-k 由后端 repository 执行，Agent 只消费已授权 chunks。
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
    # 生产使用 make provision-parser 创建只能读解析状态、回写正文并读写 chunks 的受限凭证。
    pg_dsn: str = "postgres://postgres:postgres@localhost:5432/learning_buddy"
    redis_addr: str = "localhost:6379"
    redis_timeout_s: float = 0.2
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

    agent_shared_secret: str = ""
    retriever_timeout_s: float = 0.7
    parser_embedding_timeout_s: float = 30.0
    tutor_timeout_s: float = 30.0
    query_rewrite_timeout_s: float = 0.8
    rerank_timeout_s: float = 1.2
    rerank_api_key: str = ""
    rerank_base_url: str = ""
    rerank_model: str = "qwen3-rerank"
    rerank_max_document_tokens: int = 4000
    vision_api_key: str = ""
    vision_base_url: str = ""
    vision_model: str = "qwen-vl-ocr"
    # 原始图片无法在不先做本地 OCR 的情况下应用文本脱敏，因此默认禁止发送到远端。
    vision_allow_raw_images: bool = False
    minio_enabled: bool = False
    minio_endpoint: str = "localhost:9000"
    minio_access_key: str = "minioadmin"
    minio_secret_key: str = "minioadmin"
    minio_secure: bool = False
    minio_source_bucket: str = "materials-source"
    minio_derived_bucket: str = "materials-derived"
    rag_index_version: str = "rag-v2"
    parser_version: str = "rag-v2"
    cleaning_rules_version: str = "2026-07-v1"
    max_chunk_tokens: int = 3000
    chunk_overlap_tokens: int = 300
    short_document_chars: int = 5000
    max_context_tokens: int = 12000
    port: int = 8000
    cors_origins: str = ""


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
                raise RuntimeError("material_chunks.embedding 列不存在，请确认已执行迁移")
            # pgvector 的 vector(N) 在 pg_attribute.atttypmod 中直接记录 N。
            db_dim = row[0]
            if db_dim != configured:
                raise RuntimeError(
                    f"embedding 维度不一致：配置为 {configured}，"
                    f"但 material_chunks.embedding 列为 vector({db_dim})。"
                    f"请统一 EMBEDDING_DIM 或重新执行迁移。"
                )


def embedding_dim() -> int:
    return int(os.environ.get("EMBEDDING_DIM", settings.embedding_dim))
