from __future__ import annotations

from collections.abc import Callable

import pytest

import retrieval
from db import settings
from schemas import QueryAnalysisRequest


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
