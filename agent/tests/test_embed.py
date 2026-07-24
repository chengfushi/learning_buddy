from __future__ import annotations

from typing import cast

import pytest

import embed
from db import settings


class _EmbeddingResponse:
    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, list[dict[str, list[float]]]]:
        return {"data": [{"embedding": [0.1, 0.2]}]}


def test_openai_embedder_uses_caller_timeout(monkeypatch: pytest.MonkeyPatch) -> None:
    """查询和解析路径必须能向同一 Embedder 传入不同超时预算。"""
    captured: dict[str, float] = {}

    def fake_post(*_args: object, **kwargs: object) -> _EmbeddingResponse:
        captured["timeout"] = float(cast(float, kwargs["timeout"]))
        return _EmbeddingResponse()

    monkeypatch.setattr(embed, "post_sync", fake_post)
    result = embed.OpenAIEmbedder().embed("query", timeout_s=settings.retriever_timeout_s)

    assert result == [0.1, 0.2]
    assert captured["timeout"] == settings.retriever_timeout_s


def test_openai_embedder_defaults_to_parser_timeout(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, float] = {}

    def fake_post(*_args: object, **kwargs: object) -> _EmbeddingResponse:
        captured["timeout"] = float(cast(float, kwargs["timeout"]))
        return _EmbeddingResponse()

    monkeypatch.setattr(embed, "post_sync", fake_post)
    embed.OpenAIEmbedder().embed("material chunk")

    assert captured["timeout"] == settings.parser_embedding_timeout_s
