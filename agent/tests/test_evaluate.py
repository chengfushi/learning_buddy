from __future__ import annotations

import asyncio

import pytest

import evaluate
from schemas import RerankItem, RerankResponse


def test_release_evaluation_rejects_degraded_rerank(monkeypatch: pytest.MonkeyPatch) -> None:
    async def degraded(_request: object) -> RerankResponse:
        return RerankResponse(
            items=[RerankItem(chunk_id=1, score=1.0)],
            model="rrf-order-fallback",
            degraded=True,
        )

    monkeypatch.setattr(evaluate, "rerank", degraded)
    candidates = [evaluate.Candidate(1, 10, "title", "content")]
    with pytest.raises(RuntimeError, match="release evaluation requires"):
        asyncio.run(evaluate._reranked("question", candidates))
