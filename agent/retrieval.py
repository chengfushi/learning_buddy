"""Query Rewrite、Embedding 与云端 Rerank；输入候选均已由 Backend 授权。"""

from __future__ import annotations

import asyncio
import math
import re

import httpx

from cache import cache_key, get_json, set_json
from db import settings
from embed import embed_text
from pipeline import redact_for_cloud
from schemas import (
    ChatHistory,
    QueryAnalysisRequest,
    QueryAnalysisResponse,
    RerankItem,
    RerankRequest,
    RerankResponse,
)

_FOLLOW_UP = re.compile(
    r"^(它|他|她|这个|那个|上述|前面|其中|该|那)(的|是|如何|怎么)|^(怎么|如何)(配|做|设置|处理)[？?]?$"
)


def _keywords(text: str) -> list[str]:
    terms = re.findall(r"[A-Za-z][A-Za-z0-9_.:/-]{1,}|[\u4e00-\u9fff]{2,8}", text)
    return list(dict.fromkeys(term.lower() for term in terms))[:12]


def _needs_rewrite(question: str, history: list[ChatHistory]) -> bool:
    return bool(history) and bool(_FOLLOW_UP.search(question.strip()))


async def _rewrite(question: str, history: list[ChatHistory]) -> tuple[str, bool, str]:
    if not _needs_rewrite(question, history) or not settings.llm_api_key:
        return question, False, "local"
    messages = [
        {
            "role": "system",
            "content": "把最后一个问题改写成不依赖上下文的完整检索问题，只返回改写文本。",
        }
    ]
    messages.extend(
        {"role": item.role, "content": redact_for_cloud(item.content[-2000:])}
        for item in history[-6:]
    )
    messages.append({"role": "user", "content": redact_for_cloud(question)})
    try:
        async with httpx.AsyncClient(timeout=settings.query_rewrite_timeout_s) as http:
            response = await http.post(
                f"{settings.llm_base_url.rstrip('/')}/chat/completions",
                headers={"Authorization": f"Bearer {settings.llm_api_key}"},
                json={"model": settings.llm_model, "messages": messages, "temperature": 0},
            )
        response.raise_for_status()
        rewritten = str(response.json()["choices"][0]["message"]["content"]).strip()
        return (
            (rewritten or question)[:1000],
            bool(rewritten and rewritten != question),
            settings.llm_model,
        )
    except Exception:
        return question, False, "local-fallback"


def _valid_embedding(value: object) -> list[float] | None:
    """拒绝维度错误、布尔值及非有限数，避免污染缓存和向量查询。"""
    if not isinstance(value, list) or len(value) != settings.embedding_dim:
        return None
    embedding: list[float] = []
    for item in value:
        if isinstance(item, bool) or not isinstance(item, (int, float)):
            return None
        number = float(item)
        if not math.isfinite(number):
            return None
        embedding.append(number)
    return embedding


async def _query_embedding(retrieval_query: str) -> list[float]:
    """独立缓存有效向量；临时故障返回空向量但不写入缓存。"""
    cloud_query = redact_for_cloud(retrieval_query)
    key = cache_key(
        "embedding",
        {
            "provider": settings.embedding_provider,
            "model": settings.embedding_model,
            "dimension": settings.embedding_dim,
            "query": cloud_query,
        },
    )
    cached = _valid_embedding(await get_json(key))
    if cached is not None:
        return cached
    try:
        raw_embedding = await asyncio.wait_for(
            asyncio.to_thread(embed_text, cloud_query, settings.retriever_timeout_s),
            timeout=max(0.001, settings.retriever_timeout_s),
        )
    except Exception:
        return []
    embedding = _valid_embedding(raw_embedding)
    if embedding is None:
        return []
    await set_json(key, embedding, 3600)
    return embedding


async def analyze_query(req: QueryAnalysisRequest) -> QueryAnalysisResponse:
    """分析查询并返回改写、关键词和向量；缓存不包含任何权限结果。"""
    history_payload = [
        {"role": item.role, "content": item.content[-2000:]} for item in req.history[-6:]
    ]
    key = cache_key(
        "analysis",
        {
            "model": settings.llm_model,
            "q": req.question,
            "h": history_payload,
        },
    )
    cached = await get_json(key)
    if isinstance(cached, dict):
        try:
            analysis = QueryAnalysisResponse.model_validate(cached)
        except Exception:
            analysis = None
    else:
        analysis = None
    if analysis is None:
        retrieval_query, applied, model = await _rewrite(req.question, req.history)
        analysis = QueryAnalysisResponse(
            retrieval_query=retrieval_query,
            keywords=_keywords(retrieval_query),
            rewrite_applied=applied,
            model=model,
        )
        await set_json(key, analysis.model_dump(exclude={"embedding"}), 1800)
    embedding = await _query_embedding(analysis.retrieval_query)
    return analysis.model_copy(
        update={"embedding": embedding},
    )


def _local_rerank(req: RerankRequest) -> RerankResponse:
    # candidates 已按 RRF 排序；降级时只赋单调分数，不改变权限安全的原顺序。
    items = [
        RerankItem(chunk_id=candidate.chunk_id, score=1 / (index + 1))
        for index, candidate in enumerate(req.candidates[: req.top_n])
    ]
    return RerankResponse(items=items, model="rrf-order-fallback", degraded=True)


async def rerank(req: RerankRequest) -> RerankResponse:
    """调用 qwen3-rerank；超时或协议错误时回退确定性词项重排。"""
    if not req.candidates:
        return RerankResponse(items=[], model=settings.rerank_model)
    api_key = settings.rerank_api_key or settings.embedding_api_key
    if not api_key or not settings.rerank_base_url:
        return _local_rerank(req)
    key = cache_key(
        "rerank",
        {
            "model": settings.rerank_model,
            "query": req.query,
            "top_n": req.top_n,
            "candidates": [(item.chunk_id, item.content) for item in req.candidates],
        },
    )
    cached = await get_json(key)
    if isinstance(cached, dict):
        try:
            return RerankResponse.model_validate(cached)
        except Exception:
            pass
    try:
        async with httpx.AsyncClient(timeout=settings.rerank_timeout_s) as http:
            response = await http.post(
                settings.rerank_base_url,
                headers={"Authorization": f"Bearer {api_key}"},
                json={
                    "model": settings.rerank_model,
                    "query": redact_for_cloud(req.query),
                    "documents": [
                        redact_for_cloud(item.content)[:16000] for item in req.candidates
                    ],
                    "top_n": req.top_n,
                    "instruct": "Given a technical question, retrieve passages that directly answer it.",
                },
            )
        response.raise_for_status()
        payload = response.json()
        raw_items = payload.get("results") or payload.get("output", {}).get("results") or []
        items = [
            RerankItem(
                chunk_id=req.candidates[int(item["index"])].chunk_id,
                score=float(item.get("relevance_score", item.get("score", 0))),
            )
            for item in raw_items
            if 0 <= int(item.get("index", -1)) < len(req.candidates)
        ][: req.top_n]
        if not items:
            raise ValueError("empty rerank response")
        result = RerankResponse(items=items, model=settings.rerank_model)
        await set_json(key, result.model_dump(), 3600)
        return result
    except Exception:
        return _local_rerank(req)
