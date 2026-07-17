"""资料解析与生成编排。

权限边界（engineering-standards §0 / R2）：向量检索及可见性过滤全部由 Backend
repository 完成；Agent 只消费后端传入的已授权 chunks，永远不查询权限表或拼谓词。
"""

from __future__ import annotations

import asyncio
import logging
import re
import time

from pgvector import Vector

from db import get_conn, settings
from embed import embed_text
from llm import ChunkView, MockLLM, get_llm
from schemas import PlanResult, QuizResult

logger = logging.getLogger("agent.rag")


def _chunk_text(text: str, size: int = 600, overlap: int = 80) -> list[str]:
    text = text or ""
    paras = [p.strip() for p in re.split(r"\n\s*\n", text) if p.strip()]
    if not paras:
        paras = [text.strip()]
    out: list[str] = []
    buf = ""
    for p in paras:
        if buf and len(buf) + len(p) > size:
            out.append(buf)
            buf = (buf[-overlap:] + "\n" + p) if overlap else p
        else:
            buf = (buf + "\n" + p).strip()
    if buf:
        out.append(buf)
    final: list[str] = []
    for c in out:
        if len(c) <= size:
            final.append(c)
        else:
            for i in range(0, max(1, len(c) - size), max(1, size - overlap)):
                final.append(c[i : i + size])
    return [c for c in final if c.strip()]


def _stale_parse_response(
    material_id: int,
    request_generation: int,
    current_generation: int,
    parse_status: str,
) -> dict:
    logger.info(
        "ignored stale material parse write",
        extra={
            "material_id": material_id,
            "request_generation": request_generation,
            "current_generation": current_generation,
            "parse_status": parse_status,
        },
    )
    return {"material_id": material_id, "chunks": 0, "status": "stale"}


def parse(
    material_id: int,
    parse_generation: int,
    content: str,
    file_type: str,
    storage_key: str,
) -> dict:
    """Parser 任务：仅在 Backend 当前解析代次仍有效时原子替换 chunks。"""
    del file_type, storage_key
    # 先做无锁预检，避免已知陈旧请求继续消耗远程 Embedding 配额；写入前仍会在锁内复检。
    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute(
                "SELECT parse_generation, parse_status FROM materials WHERE id = %s",
                (material_id,),
            )
            row = cur.fetchone()
            if not row:
                raise ValueError(f"material {material_id} not found")
            current_generation, parse_status = row
            if current_generation != parse_generation or parse_status != "parsing":
                return _stale_parse_response(
                    material_id,
                    parse_generation,
                    current_generation,
                    parse_status,
                )

    chunks = _chunk_text(content)
    embedded_chunks = [
        (piece, embed_text(piece, timeout_s=settings.parser_embedding_timeout_s))
        for piece in chunks
    ]

    with get_conn() as conn:
        with conn.cursor() as cur:
            # HTTP 超时不会终止 FastAPI 的同步线程。advisory lock 串行化同资料写入，
            # 行锁 + parse_generation/status 阻止超时旧请求覆盖新任务或失败终态。
            cur.execute("SELECT pg_advisory_xact_lock(%s)", (material_id,))
            cur.execute(
                "SELECT team_id, parse_generation, parse_status "
                "FROM materials WHERE id = %s FOR UPDATE",
                (material_id,),
            )
            row = cur.fetchone()
            if not row:
                raise ValueError(f"material {material_id} not found")
            team_id, current_generation, parse_status = row
            if current_generation != parse_generation or parse_status != "parsing":
                return _stale_parse_response(
                    material_id,
                    parse_generation,
                    current_generation,
                    parse_status,
                )

            # 幂等：重解析先删旧 chunk，避免重复（R3）
            cur.execute("DELETE FROM material_chunks WHERE material_id = %s", (material_id,))
            for idx, (piece, embedding) in enumerate(embedded_chunks):
                cur.execute(
                    """INSERT INTO material_chunks (team_id, material_id, chunk_idx, content, embedding)
                       VALUES (%s, %s, %s, %s, %s)""",
                    (team_id, material_id, idx, piece, Vector(embedding)),
                )
            cur.execute(
                """UPDATE materials
                   SET content = %s
                   WHERE id = %s AND parse_generation = %s AND parse_status = 'parsing'""",
                (content, material_id, parse_generation),
            )
    return {"material_id": material_id, "chunks": len(chunks), "status": "done"}


def run_chat(question: str, chunks: list[ChunkView], history=None) -> tuple[str, list[dict]]:
    answer = get_llm().chat(question, chunks, history)
    return answer, _citations(chunks)


async def run_chat_resilient(
    question: str,
    chunks: list[ChunkView],
    history: object = None,
    trace_id: str = "",
) -> tuple[str, list[dict]]:
    """按 Tutor 独立预算生成；检索超时降级已由 Backend 在权限过滤后处理。"""
    llm = get_llm()
    tutor_started = time.monotonic()
    try:
        answer = await asyncio.wait_for(
            asyncio.to_thread(llm.chat, question, chunks, history),
            timeout=max(0.001, settings.tutor_timeout_s),
        )
        logger.info(
            "tutor completed",
            extra={
                "trace_id": trace_id,
                "duration_ms": round((time.monotonic() - tutor_started) * 1000, 2),
            },
        )
    except Exception as exc:
        logger.warning(
            "tutor degraded to local mock",
            extra={
                "trace_id": trace_id,
                "duration_ms": round((time.monotonic() - tutor_started) * 1000, 2),
                "error_type": type(exc).__name__,
            },
        )
        answer = MockLLM().chat(question, chunks, history)
    return answer, _citations(chunks)


def _citations(chunks: list[ChunkView]) -> list[dict]:
    return [
        {
            "team_id": c.team_id,
            "material_id": c.material_id,
            "chapter": c.chapter,
            "chunk_idx": c.chunk_idx,
            "snippet": c.content[:120],
        }
        for c in chunks
    ]


def run_plan(goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict:
    try:
        result = get_llm().plan(goal, deadline, chunks)
        return PlanResult.model_validate(result).model_dump()
    except Exception as exc:
        logger.warning("planner degraded to local mock", extra={"error_type": type(exc).__name__})
        return MockLLM().plan(goal, deadline, chunks)


def run_quiz(topic: str, count: int, chunks: list[ChunkView]) -> list[dict]:
    try:
        result = get_llm().quiz(topic, count, chunks)
        validated = QuizResult.model_validate(result)
        return [item.model_dump() for item in validated.root]
    except Exception as exc:
        logger.warning("evaluator degraded to local mock", extra={"error_type": type(exc).__name__})
        return MockLLM().quiz(topic, count, chunks)
