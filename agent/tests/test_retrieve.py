"""R2/R3 边界测试：Agent 无检索权限面，解析写入可并发幂等。"""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor

import psycopg2
import pytest
from pgvector.psycopg2 import register_vector

import main
import rag
from db import settings
from embed import LocalEmbedder

TEAM_ID = 900002
MATERIAL_ID = 900003


def _conn():
    conn = psycopg2.connect(settings.pg_dsn, connect_timeout=5)
    register_vector(conn)
    return conn


@pytest.fixture
def parse_material():
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "INSERT INTO teams (id,name,type) VALUES (%s,'t-parse','private') "
                "ON CONFLICT (id) DO NOTHING",
                (TEAM_ID,),
            )
            cur.execute(
                "INSERT INTO materials "
                "(id, team_id, title, owner_id, parse_status, parse_generation) "
                "VALUES (%s,%s,'parse-mat',1,'parsing',1) "
                "ON CONFLICT (id) DO UPDATE "
                "SET parse_status='parsing', parse_generation=1, content=NULL",
                (MATERIAL_ID, TEAM_ID),
            )
            cur.execute("DELETE FROM material_chunks WHERE material_id=%s", (MATERIAL_ID,))
        conn.commit()
        yield
    finally:
        with conn.cursor() as cur:
            cur.execute("DELETE FROM material_chunks WHERE material_id=%s", (MATERIAL_ID,))
            cur.execute("DELETE FROM materials WHERE id=%s", (MATERIAL_ID,))
            cur.execute("DELETE FROM teams WHERE id=%s", (TEAM_ID,))
        conn.commit()
        conn.close()


def test_agent_does_not_expose_retrieval_permission_surface() -> None:
    """R2：权限检索已经收口 Backend repository，Agent 不再提供 /retrieve。"""
    paths = {route.path for route in main.app.routes}
    assert "/retrieve" not in paths
    assert "/embed" in paths


def test_parse_is_idempotent(parse_material: None, monkeypatch: pytest.MonkeyPatch) -> None:
    embedder = LocalEmbedder()
    timeouts: list[float | None] = []

    def embed_for_parse(text: str, timeout_s: float | None = None) -> list[float]:
        timeouts.append(timeout_s)
        return embedder.embed(text)

    monkeypatch.setattr(rag, "embed_text", embed_for_parse)
    text = "第一章 向量检索简介。\n\n第二章 权限模型设计。\n\n第三章 解析流水线实现。"

    rag.parse(MATERIAL_ID, 1, text, "txt", "k1")
    rag.parse(MATERIAL_ID, 1, text, "txt", "k2")

    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT count(*), count(DISTINCT chunk_idx) FROM material_chunks "
                "WHERE material_id=%s",
                (MATERIAL_ID,),
            )
            count, distinct_count = cur.fetchone()
            cur.execute("SELECT parse_status FROM materials WHERE id=%s", (MATERIAL_ID,))
            status = cur.fetchone()[0]
        assert count > 0
        assert count == distinct_count
        assert status == "parsing", "parse_status 只能由 Backend 任务状态机更新"
        assert timeouts
        assert set(timeouts) == {settings.parser_embedding_timeout_s}
    finally:
        conn.close()


def test_timed_out_retry_cannot_duplicate_chunks(
    parse_material: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    """模拟超时后的并发重试；advisory lock + 唯一索引保证无重复 chunk。"""
    embedder = LocalEmbedder()
    monkeypatch.setattr(rag, "embed_text", embedder.embed)
    text = "并发解析第一段。\n\n并发解析第二段。\n\n并发解析第三段。"

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(
            pool.map(
                lambda key: rag.parse(MATERIAL_ID, 1, text, "txt", key),
                ["old", "retry"],
            )
        )
    assert all(result["status"] == "done" for result in results)

    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT count(*), count(DISTINCT chunk_idx) FROM material_chunks "
                "WHERE material_id=%s",
                (MATERIAL_ID,),
            )
            count, distinct_count = cur.fetchone()
        assert count > 0
        assert count == distinct_count
    finally:
        conn.close()


def test_stale_parse_generation_cannot_replace_current_chunks(
    parse_material: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    """旧代次即使迟到，也不能删除或覆盖当前资料的 chunks/content。"""
    embedder = LocalEmbedder()
    embed_calls = 0

    def recording_embed(text: str, timeout_s: float | None = None) -> list[float]:
        nonlocal embed_calls
        embed_calls += 1
        return embedder.embed(text, timeout_s)

    monkeypatch.setattr(rag, "embed_text", recording_embed)

    current = rag.parse(MATERIAL_ID, 1, "当前版本内容", "txt", "current")
    calls_after_current = embed_calls
    stale = rag.parse(MATERIAL_ID, 0, "陈旧版本内容", "txt", "stale")

    assert current["status"] == "done"
    assert stale["status"] == "stale"
    assert embed_calls == calls_after_current, "陈旧任务应在远程 Embedding 前被拒绝"
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute("SELECT content FROM materials WHERE id=%s", (MATERIAL_ID,))
            material_content = cur.fetchone()[0]
            cur.execute(
                "SELECT string_agg(content, ' ') FROM material_chunks WHERE material_id=%s",
                (MATERIAL_ID,),
            )
            chunk_content = cur.fetchone()[0]
        assert material_content == "当前版本内容"
        assert "当前版本内容" in chunk_content
        assert "陈旧版本内容" not in chunk_content
    finally:
        conn.close()
