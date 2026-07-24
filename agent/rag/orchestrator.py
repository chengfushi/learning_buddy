"""资料解析与生成编排。

权限边界：向量检索及可见性过滤全部由 Backend repository 完成；
Agent 只消费后端传入的已授权 chunks，永远不查询权限表或拼谓词。
"""

from __future__ import annotations

import asyncio
import logging
import time
from typing import Any

from pgvector import Vector
from psycopg2.extras import Json

from core.config import settings
from core.db import get_conn
from core.utils import redact_for_cloud
from models import ChunkView, PlanResult, QuizResult
from rag.pipeline import process_document
from services.embed import embed_text
from services.llm import NO_EVIDENCE_RESPONSE, MockLLM, get_llm

logger = logging.getLogger("agent.rag")


def _stale_parse_response(
    material_id: int,
    request_generation: int,
    current_generation: int,
    parse_status: str,
) -> dict[str, int | str]:
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


def _record_processing_stage(
    material_id: int,
    parse_generation: int,
    stage: str,
    progress: dict[str, int | str],
    status: str = "running",
    error: str | None = None,
) -> None:
    try:
        with get_conn() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """INSERT INTO rag_processing_runs
                       (material_id, parse_generation, index_version, stage, status,
                        parser_version, cleaning_rules_version, progress, error, finished_at)
                       VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,
                               CASE WHEN %s IN ('done','failed','stale') THEN now() ELSE NULL END)
                       ON CONFLICT (material_id, parse_generation, index_version) DO UPDATE SET
                         stage=EXCLUDED.stage, status=EXCLUDED.status, progress=EXCLUDED.progress,
                         error=EXCLUDED.error, finished_at=EXCLUDED.finished_at""",
                    (
                        material_id,
                        parse_generation,
                        settings.rag_index_version,
                        stage,
                        status,
                        settings.parser_version,
                        settings.cleaning_rules_version,
                        Json(progress),
                        error,
                        status,
                    ),
                )
    except Exception as exc:
        logger.warning(
            "processing stage persistence degraded",
            extra={"material_id": material_id, "error_type": type(exc).__name__},
        )


def parse(
    material_id: int,
    parse_generation: int,
    content: str,
    file_type: str,
    storage_key: str,
) -> dict[str, int | str]:
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

    try:
        result = process_document(
            material_id,
            parse_generation,
            content,
            file_type,
            storage_key,
            stage_callback=lambda stage, progress: _record_processing_stage(
                material_id, parse_generation, stage, progress
            ),
        )
        _record_processing_stage(
            material_id,
            parse_generation,
            "embed",
            {"chunk_count": len(result.chunks)},
        )
        embedded_chunks = [
            (
                chunk,
                embed_text(
                    redact_for_cloud(chunk.content),
                    timeout_s=settings.parser_embedding_timeout_s,
                ),
            )
            for chunk in result.chunks
        ]
    except Exception as exc:
        _record_processing_stage(
            material_id,
            parse_generation,
            "pipeline",
            {},
            status="failed",
            error=str(exc)[:2000],
        )
        raise

    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT pg_advisory_xact_lock(%s)", (material_id,))
            cur.execute(
                "SELECT team_id, title, parse_generation, parse_status "
                "FROM materials WHERE id = %s FOR UPDATE",
                (material_id,),
            )
            row = cur.fetchone()
            if not row:
                raise ValueError(f"material {material_id} not found")
            team_id, title, current_generation, parse_status = row
            if current_generation != parse_generation or parse_status != "parsing":
                return _stale_parse_response(
                    material_id,
                    parse_generation,
                    current_generation,
                    parse_status,
                )

            cur.execute(
                "DELETE FROM material_chunks WHERE material_id = %s AND index_version = %s",
                (material_id, settings.rag_index_version),
            )
            cur.execute(
                "DELETE FROM material_assets WHERE material_id = %s AND index_version = %s",
                (material_id, settings.rag_index_version),
            )
            asset_ids: dict[int, int] = {}
            for asset_index, asset in enumerate(result.assets):
                if not asset.storage_key:
                    continue
                cur.execute(
                    """INSERT INTO material_assets
                       (material_id, parse_generation, index_version, storage_key, sha256,
                        mime_type, page_number, ocr_text, caption, width, height)
                       VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) RETURNING id""",
                    (
                        material_id,
                        parse_generation,
                        settings.rag_index_version,
                        asset.storage_key,
                        asset.sha256,
                        asset.mime_type,
                        asset.page_number,
                        asset.ocr_text or None,
                        asset.caption or None,
                        asset.width,
                        asset.height,
                    ),
                )
                asset_ids[asset_index] = cur.fetchone()[0]
            for chunk, embedding in embedded_chunks:
                asset_id = (
                    asset_ids.get(chunk.asset_index) if chunk.asset_index is not None else None
                )
                lexical_text = " ".join(
                    [title, *result.keywords, chunk.heading_path, chunk.lexical_text]
                ).strip()
                cur.execute(
                    """INSERT INTO material_chunks
                       (team_id, material_id, index_version, kind, chunk_idx, content, embedding,
                        heading_path, page_number, token_count, lexical_text, asset_id)
                       VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)""",
                    (
                        team_id,
                        material_id,
                        settings.rag_index_version,
                        chunk.kind,
                        chunk.chunk_idx,
                        chunk.content,
                        Vector(embedding),
                        chunk.heading_path or None,
                        chunk.page_number,
                        chunk.token_count,
                        lexical_text,
                        asset_id,
                    ),
                )
            cur.execute(
                "SELECT EXISTS (SELECT 1 FROM material_chunks "
                "WHERE material_id = %s AND index_version = 'legacy-v1')",
                (material_id,),
            )
            has_legacy = bool(cur.fetchone()[0])
            cur.execute(
                "SELECT EXISTS (SELECT 1 FROM rag_index_versions "
                "WHERE version = %s AND status = 'active')",
                (settings.rag_index_version,),
            )
            v2_is_active = bool(cur.fetchone()[0])
            visible_index_version = (
                settings.rag_index_version if v2_is_active or not has_legacy else "legacy-v1"
            )
            cur.execute(
                """UPDATE materials
                   SET content = %s, summary = %s, semantic_keywords = %s,
                       suggested_questions = %s, normalized_storage_key = %s,
                       parser_version = %s, index_version = %s, cleaning_stats = %s
                   WHERE id = %s AND parse_generation = %s AND parse_status = 'parsing'""",
                (
                    result.markdown,
                    result.summary,
                    result.keywords,
                    result.questions,
                    result.normalized_storage_key or None,
                    settings.parser_version,
                    visible_index_version,
                    Json(result.cleaning_stats),
                    material_id,
                    parse_generation,
                ),
            )
    _record_processing_stage(
        material_id,
        parse_generation,
        "persist",
        {"chunk_count": len(result.chunks), "asset_count": len(asset_ids)},
        status="done",
    )
    return {"material_id": material_id, "chunks": len(result.chunks), "status": "done"}


def run_chat(
    question: str, chunks: list[ChunkView], history: Any = None
) -> tuple[str, list[dict[str, Any]]]:
    evidence = _evidence_chunks(chunks)
    if not evidence:
        return NO_EVIDENCE_RESPONSE, []
    answer = get_llm().chat(question, evidence, history)
    return answer, _citations(evidence)


async def run_chat_resilient(
    question: str,
    chunks: list[ChunkView],
    history: object = None,
    trace_id: str = "",
) -> tuple[str, list[dict[str, Any]]]:
    evidence = _evidence_chunks(chunks)
    if not evidence:
        logger.info("tutor refused without evidence", extra={"trace_id": trace_id})
        return NO_EVIDENCE_RESPONSE, []
    llm = get_llm()
    tutor_started = time.monotonic()
    try:
        answer = await asyncio.wait_for(
            asyncio.to_thread(llm.chat, question, evidence, history),
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
        answer = MockLLM().chat(question, evidence, history)
    return answer, _citations(evidence)


def _evidence_chunks(chunks: list[ChunkView]) -> list[ChunkView]:
    return [chunk for chunk in chunks if chunk.content.strip()]


def _citations(chunks: list[ChunkView]) -> list[dict[str, Any]]:
    return [
        {
            "team_id": c.team_id,
            "material_id": c.material_id,
            "chapter": c.chapter,
            "chunk_idx": c.chunk_idx,
            "snippet": c.content[:120],
            "chunk_id": c.chunk_id,
            "title": c.title,
            "kind": c.kind,
            "page_number": c.page_number,
            "score": c.score,
            "asset_id": c.asset_id,
        }
        for c in chunks
    ]


def run_plan(goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict[str, Any]:
    try:
        result = get_llm().plan(goal, deadline, chunks)
        return PlanResult.model_validate(result).model_dump()
    except Exception as exc:
        logger.warning("planner degraded to local mock", extra={"error_type": type(exc).__name__})
        return MockLLM().plan(goal, deadline, chunks)


def run_quiz(topic: str, count: int, chunks: list[ChunkView]) -> list[dict[str, Any]]:
    try:
        result = get_llm().quiz(topic, count, chunks)
        validated = QuizResult.model_validate(result)
        return [item.model_dump() for item in validated.root]
    except Exception as exc:
        logger.warning("evaluator degraded to local mock", extra={"error_type": type(exc).__name__})
        return MockLLM().quiz(topic, count, chunks)
