from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

import main
from core.auth import AGENT_TOKEN_HEADER, assert_agent_auth_config
from core.config import settings

PROTECTED_PATHS = ["/parse", "/embed", "/chat", "/plan", "/quiz"]
TEST_SECRET = "unit-test-agent-secret"


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setattr(settings, "agent_shared_secret", TEST_SECRET)
    return TestClient(main.app)


@pytest.mark.parametrize("path", PROTECTED_PATHS)
@pytest.mark.parametrize("headers", [{}, {AGENT_TOKEN_HEADER: "wrong-secret"}])
def test_protected_routes_reject_missing_or_invalid_token(
    client: TestClient,
    path: str,
    headers: dict[str, str],
) -> None:
    response = client.post(path, json={}, headers=headers)
    assert response.status_code == 401
    assert response.json() == {"detail": "invalid agent service credential"}


def test_protected_route_accepts_backend_token(
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(main, "parse", lambda *_args: {"status": "done"})

    response = client.post(
        "/parse",
        json={"material_id": 42, "parse_generation": 1, "content": "authenticated"},
        headers={AGENT_TOKEN_HEADER: TEST_SECRET},
    )

    assert response.status_code == 200
    assert response.json() == {"status": "done"}


def test_health_remains_public(client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(main, "health_ok", lambda: True)

    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_startup_rejects_empty_shared_secret(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(settings, "agent_shared_secret", "")

    with pytest.raises(RuntimeError, match="AGENT_SHARED_SECRET"):
        assert_agent_auth_config()
