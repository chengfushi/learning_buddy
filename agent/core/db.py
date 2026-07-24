"""数据库连接与 pgvector 适配。

安全边界：检索可见性与 pgvector top-k 由后端 repository 执行，Agent 只消费已授权 chunks。
"""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager

import psycopg2
from pgvector.psycopg2 import register_vector

from core.config import embedding_dim, settings


@contextmanager
def get_conn() -> Iterator[psycopg2.extensions.connection]:
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
            db_dim = row[0]
            if db_dim != configured:
                raise RuntimeError(
                    f"embedding 维度不一致：配置为 {configured}，"
                    f"但 material_chunks.embedding 列为 vector({db_dim})。"
                    f"请统一 EMBEDDING_DIM 或重新执行迁移。"
                )
