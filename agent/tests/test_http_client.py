from __future__ import annotations

import httpx
import pytest

import http_client
from db import settings


class _Response:
    def __init__(self, status_code: int) -> None:
        self.status_code = status_code
        self.request = httpx.Request("POST", "https://model.test")

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            raise httpx.HTTPStatusError(
                f"status={self.status_code}", request=self.request, response=self._http_response()
            )

    def _http_response(self) -> httpx.Response:
        return httpx.Response(self.status_code, request=self.request)


class _SyncClient:
    def __init__(self, responses: list[_Response]) -> None:
        self.responses = responses
        self.calls = 0

    def post(self, *_args: object, **_kwargs: object) -> _Response:
        response = self.responses[min(self.calls, len(self.responses) - 1)]
        self.calls += 1
        return response


def test_sync_post_retries_server_errors_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _SyncClient([_Response(503), _Response(200)])
    monkeypatch.setattr(http_client, "get_sync_client", lambda: client)
    monkeypatch.setattr(settings, "external_retry_attempts", 1)
    monkeypatch.setattr(settings, "external_retry_backoff_s", 0)
    http_client._circuits.clear()

    response = http_client.post_sync(
        "https://model.test/chat",
        headers={},
        json={},
        timeout=1,
    )

    assert response.status_code == 200
    assert client.calls == 2


def test_sync_post_does_not_retry_client_errors(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _SyncClient([_Response(400)])
    monkeypatch.setattr(http_client, "get_sync_client", lambda: client)
    monkeypatch.setattr(settings, "external_retry_attempts", 3)
    http_client._circuits.clear()

    with pytest.raises(httpx.HTTPStatusError):
        http_client.post_sync("https://model.test/chat", headers={}, json={}, timeout=1)

    assert client.calls == 1
