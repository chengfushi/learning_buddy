"""数据库启动断言测试（R1）。"""

from __future__ import annotations

from collections.abc import Callable, Iterator
from contextlib import AbstractContextManager, contextmanager

import pytest

import core.config as config
import core.db as db


class _FakeCursor:
    def __init__(self, typmod: int | None) -> None:
        self._typmod = typmod

    def __enter__(self) -> _FakeCursor:
        return self

    def __exit__(self, *_args: object) -> None:
        return None

    def execute(self, _query: str) -> None:
        return None

    def fetchone(self) -> tuple[int] | None:
        if self._typmod is None:
            return None
        return (self._typmod,)


class _FakeConnection:
    def __init__(self, typmod: int | None) -> None:
        self._typmod = typmod

    def cursor(self) -> _FakeCursor:
        return _FakeCursor(self._typmod)


def _fake_get_conn(typmod: int | None) -> Callable[[], AbstractContextManager[_FakeConnection]]:
    @contextmanager
    def get_conn() -> Iterator[_FakeConnection]:
        yield _FakeConnection(typmod)

    return get_conn


def test_assert_embedding_dim_accepts_pgvector_typmod_directly(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """pgvector atttypmod 已是 N，不得按 PostgreSQL 通用 varlena 规则减 4。"""
    monkeypatch.setattr(config, "embedding_dim", lambda: 1024)
    monkeypatch.setattr(db, "get_conn", _fake_get_conn(1024))

    db.assert_embedding_dim()


def test_assert_embedding_dim_rejects_real_mismatch(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(config, "embedding_dim", lambda: 1024)
    monkeypatch.setattr(db, "get_conn", _fake_get_conn(768))

    with pytest.raises(RuntimeError, match=r"vector\(768\)"):
        db.assert_embedding_dim()
