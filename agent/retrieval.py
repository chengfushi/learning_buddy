"""Query Rewrite、Embedding 与云端 Rerank；输入候选均已由 Backend 授权。"""

from __future__ import annotations

import asyncio
import math
import re

import httpx

__all__ = ["estimate_tokens", "httpx"]

from cache import cache_key, get_json, set_json
from db import settings
from embed import embed_text
from pipeline import estimate_tokens, redact_for_cloud
from schemas import (
    ChatHistory,
    QueryAnalysisRequest,
    QueryAnalysisResponse,
    RerankItem,
    RerankRequest,
    RerankResponse,
)

_CONTEXTUAL_REFERENCE = re.compile(
    r"^(?:那|那么|然后|它|他|她|其|这个|那个|这些|那些|上述|上面|前面|其中|"
    r"(?:该|此)(?:参数|配置|功能|服务|接口|模块|组件|系统|方案|文档|命令|错误|问题|"
    r"规则|步骤|版本|字段|选项))"
)
_ELLIPTICAL_QUESTION = re.compile(
    r"^(?:怎么|如何)(?:配|做|办|配置|设置|处理|解决|修改|使用|部署|排查)[？?]?$"
)


def _keywords(text: str) -> list[str]:
    terms = re.findall(r"[A-Za-z][A-Za-z0-9_.:/-]{1,}|[\u4e00-\u9fff]{2,8}", text)
    return list(dict.fromkeys(term.lower() for term in terms))[:12]


def _needs_rewrite(question: str, history: list[ChatHistory]) -> bool:
    if not history:
        return False
    normalized = question.strip()
    return bool(
        _CONTEXTUAL_REFERENCE.search(normalized) or _ELLIPTICAL_QUESTION.fullmatch(normalized)
    )


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


def _truncate_rerank_document(text: str) -> str:
    """按模型 Token 上限保守截断，避免一个超长旧块拖垮整次重排。"""
    limit = max(1, settings.rerank_max_document_tokens)
    max_chars = limit * 4
    if len(text) <= max_chars and estimate_tokens(text) <= limit:
        return text
    low, high = 0, min(len(text), max_chars)
    while low < high:
        middle = (low + high + 1) // 2
        if estimate_tokens(text[:middle]) <= limit:
            low = middle
        else:
            high = middle - 1
    return text[:low]


def _valid_cached_rerank(req: RerankRequest, response: RerankResponse) -> RerankResponse | None:
    expected = min(req.top_n, len(req.candidates))
    candidate_ids = {candidate.chunk_id for candidate in req.candidates}
    item_ids = [item.chunk_id for item in response.items]
    if (
        response.degraded
        or response.model != settings.rerank_model
        or len(item_ids) != expected
        or len(set(item_ids)) != expected
        or not set(item_ids).issubset(candidate_ids)
        or any(not math.isfinite(item.score) for item in response.items)
    ):
        return None
    return response


def _remote_rerank_items(req: RerankRequest, raw_items: object) -> list[RerankItem] | None:
    expected = min(req.top_n, len(req.candidates))
    if not isinstance(raw_items, list) or len(raw_items) < expected:
        return None
    seen_indexes: set[int] = set()
    items: list[RerankItem] = []
    for raw_item in raw_items[:expected]:
        if not isinstance(raw_item, dict):
            return None
        index = raw_item.get("index")
        score = raw_item.get("relevance_score", raw_item.get("score"))
        if (
            isinstance(index, bool)
            or not isinstance(index, int)
            or index < 0
            or index >= len(req.candidates)
            or index in seen_indexes
            or isinstance(score, bool)
            or not isinstance(score, (int, float))
            or not math.isfinite(float(score))
        ):
            return None
        seen_indexes.add(index)
        items.append(
            RerankItem(
                chunk_id=req.candidates[index].chunk_id,
                score=float(score),
            )
        )
    return items


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
            cached_response = _valid_cached_rerank(req, RerankResponse.model_validate(cached))
            if cached_response is not None:
                return cached_response
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
                        _truncate_rerank_document(redact_for_cloud(item.content))
                        for item in req.candidates
                    ],
                    "top_n": req.top_n,
                    "instruct": "Given a technical question, retrieve passages that directly answer it.",
                },
            )
        response.raise_for_status()
        payload = response.json()
        raw_items = payload.get("results") or payload.get("output", {}).get("results") or []
        items = _remote_rerank_items(req, raw_items)
        if items is None:
            raise ValueError("invalid or incomplete rerank response")
        result = RerankResponse(items=items, model=settings.rerank_model)
        await set_json(key, result.model_dump(), 3600)
        return result
    except Exception:
        return _local_rerank(req)
