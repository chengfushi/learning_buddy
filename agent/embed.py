"""文本向量化。

默认使用确定性本地嵌入（hashing trick → 768 维，L2 归一化）：
- 完全离线、无外部模型下载，保证本地链路可跑通；
- 维度恒定 = EMBEDDING_DIM，满足 engineering-standards R1（维度一致性）；
- 同一文本永远得到同一向量，解析与检索可复现。

若设置 EMBEDDING_PROVIDER=openai 且 EMBEDDING_API_KEY 非空，则走 OpenAI 兼容嵌入接口
（维度仍需与库表一致，由调用方保证）。
"""

from __future__ import annotations

import hashlib
import re
from collections.abc import Iterator
from typing import cast

import httpx
import numpy as np

from db import settings


class Embedder:
    def embed(self, text: str, timeout_s: float | None = None) -> list[float]:
        raise NotImplementedError

    def dim(self) -> int:
        return settings.embedding_dim


class LocalEmbedder(Embedder):
    """哈希袋特征嵌入：对 ascii 词、CJK 单字与相邻双字分别哈希到维度空间累加，再 L2 归一化。"""

    def __init__(self, dim: int | None = None, seed: int = 0) -> None:
        self._dim = dim or settings.embedding_dim
        self._seed = seed

    def _tokens(self, text: str) -> Iterator[str]:
        text = (text or "").lower()
        yield from re.findall(r"[a-z0-9]+", text)
        cjk = re.findall(r"[一-鿿]", text)
        yield from (f"c:{ch}" for ch in cjk)
        yield from (f"b:{cjk[i]}{cjk[i + 1]}" for i in range(len(cjk) - 1))

    def _hash_idx(self, token: str) -> int:
        digest = hashlib.md5(f"{self._seed}:{token}".encode()).digest()
        return int.from_bytes(digest[:4], "big") % self._dim

    def embed(self, text: str, timeout_s: float | None = None) -> list[float]:
        del timeout_s
        vec = np.zeros(self._dim, dtype=np.float32)
        for tok in self._tokens(text):
            vec[self._hash_idx(tok)] += 1.0
        norm = float(np.linalg.norm(vec))
        if norm > 0:
            vec = vec / norm
        return vec.tolist()


class OpenAIEmbedder(Embedder):
    """OpenAI 兼容嵌入（可选，默认不启用）。维度须与库表一致。"""

    def embed(self, text: str, timeout_s: float | None = None) -> list[float]:
        effective_timeout = (
            timeout_s if timeout_s is not None else settings.parser_embedding_timeout_s
        )
        resp = httpx.post(
            f"{settings.embedding_base_url.rstrip('/')}/embeddings",
            headers={"Authorization": f"Bearer {settings.embedding_api_key}"},
            json={
                "model": settings.embedding_model,
                "input": text,
                "dimensions": settings.embedding_dim,
            },
            timeout=effective_timeout,
        )
        resp.raise_for_status()
        payload = cast(dict[str, list[dict[str, list[float]]]], resp.json())
        return payload["data"][0]["embedding"]


def get_embedder() -> Embedder:
    if settings.embedding_provider == "openai" and settings.embedding_api_key:
        return OpenAIEmbedder()
    return LocalEmbedder()


_embedder = get_embedder()


def embed_text(text: str, timeout_s: float | None = None) -> list[float]:
    """统一文本向量化入口；调用方按查询或解析路径传入独立超时预算。"""
    return _embedder.embed(text, timeout_s)
