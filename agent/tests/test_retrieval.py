from __future__ import annotations

from collections.abc import Callable

import pytest

import retrieval
from db import settings
from schemas import QueryAnalysisRequest, RerankCandidate, RerankRequest


def _install_memory_cache(monkeypatch: pytest.MonkeyPatch) -> dict[str, object]:
    values: dict[str, object] = {}

    async def get_json(key: str) -> object:
        return values.get(key)

    async def set_json(key: str, value: object, _ttl_s: int) -> None:
        values[key] = value

    monkeypatch.setattr(retrieval, "get_json", get_json)
    monkeypatch.setattr(retrieval, "set_json", set_json)
    return values


def _install_rewrite(
    monkeypatch: pytest.MonkeyPatch,
) -> Callable[[], int]:
    calls = 0

    async def rewrite(_question: str, _history: object) -> tuple[str, bool, str]:
        nonlocal calls
        calls += 1
        return "MQTT 如何配置？", True, "rewrite-model"

    monkeypatch.setattr(retrieval, "_rewrite", rewrite)
    return lambda: calls


@pytest.mark.anyio
async def test_transient_embedding_failure_is_not_cached(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    values = _install_memory_cache(monkeypatch)
    rewrite_calls = _install_rewrite(monkeypatch)
    monkeypatch.setattr(settings, "embedding_dim", 3)
    embed_calls = 0

    def embed_text(_text: str, _timeout_s: float) -> list[float]:
        nonlocal embed_calls
        embed_calls += 1
        if embed_calls == 1:
            raise TimeoutError("temporary outage")
        return [0.1, 0.2, 0.3]

    monkeypatch.setattr(retrieval, "embed_text", embed_text)
    request = QueryAnalysisRequest(question="它怎么配？")

    degraded = await retrieval.analyze_query(request)
    recovered = await retrieval.analyze_query(request)

    assert degraded.embedding == []
    assert recovered.embedding == [0.1, 0.2, 0.3]
    assert rewrite_calls() == 1
    assert embed_calls == 2
    analysis_values = [value for key, value in values.items() if key.startswith("rag:analysis:")]
    assert analysis_values and "embedding" not in analysis_values[0]


@pytest.mark.anyio
async def test_valid_embedding_cache_is_reused_independently(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _install_memory_cache(monkeypatch)
    rewrite_calls = _install_rewrite(monkeypatch)
    monkeypatch.setattr(settings, "embedding_dim", 3)
    embed_calls = 0

    def embed_text(_text: str, _timeout_s: float) -> list[float]:
        nonlocal embed_calls
        embed_calls += 1
        return [0.4, 0.5, 0.6]

    monkeypatch.setattr(retrieval, "embed_text", embed_text)
    request = QueryAnalysisRequest(question="它怎么配？")

    first = await retrieval.analyze_query(request)
    second = await retrieval.analyze_query(request)

    assert first.embedding == second.embedding == [0.4, 0.5, 0.6]
    assert rewrite_calls() == 1
    assert embed_calls == 1


@pytest.mark.anyio
async def test_invalid_provider_embedding_is_not_cached(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    values = _install_memory_cache(monkeypatch)
    _install_rewrite(monkeypatch)
    monkeypatch.setattr(settings, "embedding_dim", 3)
    monkeypatch.setattr(retrieval, "embed_text", lambda *_args: [0.1, float("nan"), 0.3])

    result = await retrieval.analyze_query(QueryAnalysisRequest(question="它怎么配？"))

    assert result.embedding == []
    assert not any(key.startswith("rag:embedding:") for key in values)


class _RerankResponse:
    def __init__(self, payload: dict[str, object]) -> None:
        self.payload = payload

    def raise_for_status(self) -> None:
        return

    def json(self) -> dict[str, object]:
        return self.payload


class _RerankClient:
    def __init__(self, payload: dict[str, object]) -> None:
        self.payload = payload

    async def __aenter__(self) -> _RerankClient:
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    async def post(self, *_args: object, **_kwargs: object) -> _RerankResponse:
        return _RerankResponse(self.payload)


def _rerank_request() -> RerankRequest:
    return RerankRequest(
        query="MQTT 如何配置？",
        candidates=[
            RerankCandidate(chunk_id=11, material_id=1, content="候选一"),
            RerankCandidate(chunk_id=12, material_id=2, content="候选二"),
        ],
        top_n=2,
    )


@pytest.mark.anyio
@pytest.mark.parametrize(
    "results",
    [
        [{"index": 0, "relevance_score": 0.9}],
        [
            {"index": 0, "relevance_score": 0.9},
            {"index": 0, "relevance_score": 0.8},
        ],
        [
            {"index": 0, "relevance_score": 0.9},
            {"index": 1, "relevance_score": float("nan")},
        ],
    ],
)
async def test_invalid_remote_rerank_results_fall_back_to_rrf(
    monkeypatch: pytest.MonkeyPatch,
    results: list[dict[str, object]],
) -> None:
    cache_writes: list[object] = []

    async def get_json(_key: str) -> None:
        return None

    async def set_json(_key: str, value: object, _ttl_s: int) -> None:
        cache_writes.append(value)

    monkeypatch.setattr(retrieval, "get_json", get_json)
    monkeypatch.setattr(retrieval, "set_json", set_json)
    monkeypatch.setattr(settings, "rerank_api_key", "test-key")
    monkeypatch.setattr(settings, "rerank_base_url", "https://rerank.invalid")
    monkeypatch.setattr(
        retrieval.httpx,
        "AsyncClient",
        lambda **_kwargs: _RerankClient({"results": results}),
    )

    response = await retrieval.rerank(_rerank_request())

    assert response.degraded is True
    assert response.model == "rrf-order-fallback"
    assert [item.chunk_id for item in response.items] == [11, 12]
    assert cache_writes == []


@pytest.mark.anyio
async def test_invalid_cached_rerank_is_ignored(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    cached = {
        "items": [{"chunk_id": 11, "score": 0.9}, {"chunk_id": 11, "score": 0.8}],
        "model": settings.rerank_model,
        "degraded": False,
    }

    async def get_json(_key: str) -> dict[str, object]:
        return cached

    async def set_json(_key: str, _value: object, _ttl_s: int) -> None:
        return None

    monkeypatch.setattr(retrieval, "get_json", get_json)
    monkeypatch.setattr(retrieval, "set_json", set_json)
    monkeypatch.setattr(settings, "rerank_api_key", "test-key")
    monkeypatch.setattr(settings, "rerank_base_url", "https://rerank.invalid")
    monkeypatch.setattr(
        retrieval.httpx,
        "AsyncClient",
        lambda **_kwargs: _RerankClient(
            {
                "results": [
                    {"index": 1, "relevance_score": 0.95},
                    {"index": 0, "relevance_score": 0.75},
                ]
            }
        ),
    )

    response = await retrieval.rerank(_rerank_request())

    assert response.degraded is False
    assert [item.chunk_id for item in response.items] == [12, 11]
