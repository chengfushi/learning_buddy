"""应用配置管理（Pydantic Settings）。

安全边界：Agent 不持有权限表凭证，仅管理自身连接与模型配置。
"""

from __future__ import annotations

import os

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    pg_dsn: str = "postgres://postgres:postgres@localhost:5432/learning_buddy"
    redis_addr: str = "localhost:6379"
    redis_timeout_s: float = 0.2
    external_max_connections: int = 32
    external_keepalive_connections: int = 16
    external_retry_attempts: int = 2
    external_retry_backoff_s: float = 0.1
    external_circuit_failures: int = 3
    external_circuit_reset_s: float = 10.0
    embedding_dim: int = 1024
    embedding_provider: str = "openai"

    llm_api_key: str = ""
    llm_base_url: str = "https://api.deepseek.com/v1"
    llm_model: str = "deepseek-chat"

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


def embedding_dim() -> int:
    return int(os.environ.get("EMBEDDING_DIM", settings.embedding_dim))
