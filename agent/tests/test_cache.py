from __future__ import annotations

import pytest

import cache
from db import settings


def test_redis_client_has_bounded_connect_and_socket_timeouts(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(cache, "_redis", None)
    monkeypatch.setattr(settings, "redis_timeout_s", 0.35)
    redis = cache.client()
    options = redis.connection_pool.connection_kwargs
    assert options["socket_connect_timeout"] == 0.35
    assert options["socket_timeout"] == 0.35
    assert options["retry_on_timeout"] is False
    monkeypatch.setattr(cache, "_redis", None)
