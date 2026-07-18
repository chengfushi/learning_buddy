from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

import main
import rag
from auth import AGENT_TOKEN_HEADER
from db import settings
from schemas import (
    ChatRequest,
    EmbedRequest,
    ParseRequest,
    PlanRequest,
    QueryAnalysisRequest,
    QueryAnalysisResponse,
    QuizRequest,
    RerankRequest,
    RerankResponse,
)

CONTRACT_PATH = Path(__file__).resolve().parents[2] / "tests" / "contracts" / "agent_api.json"
TEST_SECRET = "contract-agent-secret"


@pytest.fixture
def contract() -> dict[str, object]:
    return json.loads(CONTRACT_PATH.read_text(encoding="utf-8"))


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch) -> TestClient:
    monkeypatch.setattr(settings, "agent_shared_secret", TEST_SECRET)
    return TestClient(main.app)


def test_contract_requests_match_pydantic_models(contract: dict[str, object]) -> None:
    models = {
        "parse": ParseRequest,
        "embed": EmbedRequest,
        "analyze_query": QueryAnalysisRequest,
        "rerank": RerankRequest,
        "chat": ChatRequest,
        "plan": PlanRequest,
        "quiz": QuizRequest,
    }
    for name, model in models.items():
        exchange = contract[name]
        assert isinstance(exchange, dict)
        model.model_validate(exchange["request"])


def test_contract_matches_fastapi_responses(
    contract: dict[str, object],
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    headers = {AGENT_TOKEN_HEADER: TEST_SECRET}

    parse_exchange = contract["parse"]
    assert isinstance(parse_exchange, dict)
    monkeypatch.setattr(main, "parse", lambda *_args: parse_exchange["response"])
    response = client.post("/parse", json=parse_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == parse_exchange["response"]

    embed_exchange = contract["embed"]
    assert isinstance(embed_exchange, dict)
    embed_response = embed_exchange["response"]
    assert isinstance(embed_response, dict)
    monkeypatch.setattr(
        main,
        "embed_text",
        lambda *_args, **_kwargs: embed_response["embedding"],
    )
    response = client.post("/embed", json=embed_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == embed_response

    analyze_exchange = contract["analyze_query"]
    assert isinstance(analyze_exchange, dict)

    async def fake_analyze(_request: object) -> object:
        return QueryAnalysisResponse.model_validate(analyze_exchange["response"])

    monkeypatch.setattr(main, "analyze_query", fake_analyze)
    response = client.post("/analyze-query", json=analyze_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == analyze_exchange["response"]

    rerank_exchange = contract["rerank"]
    assert isinstance(rerank_exchange, dict)

    async def fake_rerank(_request: object) -> object:
        return RerankResponse.model_validate(rerank_exchange["response"])

    monkeypatch.setattr(main, "rerank", fake_rerank)
    response = client.post("/rerank", json=rerank_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == rerank_exchange["response"]

    plan_exchange = contract["plan"]
    assert isinstance(plan_exchange, dict)
    monkeypatch.setattr(main, "run_plan", lambda *_args: plan_exchange["response"])
    response = client.post("/plan", json=plan_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == plan_exchange["response"]

    quiz_exchange = contract["quiz"]
    assert isinstance(quiz_exchange, dict)
    monkeypatch.setattr(main, "run_quiz", lambda *_args: quiz_exchange["response"])
    response = client.post("/quiz", json=quiz_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.json() == quiz_exchange["response"]

    chat_exchange = contract["chat"]
    assert isinstance(chat_exchange, dict)
    chat_response = chat_exchange["response"]
    assert isinstance(chat_response, dict)

    async def fake_chat(*_args: object, **_kwargs: object) -> tuple[str, list[dict]]:
        return chat_response["answer"], chat_response["citations"]

    monkeypatch.setattr(main, "run_chat_resilient", fake_chat)
    response = client.post("/chat", json=chat_exchange["request"], headers=headers)
    assert response.status_code == 200
    assert response.headers["X-Trace-ID"]
    events = [
        json.loads(line.removeprefix("data: "))
        for line in response.text.splitlines()
        if line.startswith("data: ")
    ]
    answer = "".join(event.get("text", "") for event in events if event["type"] == "token")
    done = next(event for event in events if event["type"] == "done")
    assert answer == chat_response["answer"]
    assert done["citations"] == chat_response["citations"]


def test_chat_without_evidence_streams_grounded_refusal(
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def unexpected_llm() -> object:
        raise AssertionError("tutor must not be called without evidence")

    monkeypatch.setattr(rag, "get_llm", unexpected_llm)
    response = client.post(
        "/chat",
        json={"question": "没有召回时也回答吗？", "chunks": [], "service": "chat"},
        headers={AGENT_TOKEN_HEADER: TEST_SECRET},
    )

    assert response.status_code == 200
    events = [
        json.loads(line.removeprefix("data: "))
        for line in response.text.splitlines()
        if line.startswith("data: ")
    ]
    answer = "".join(event.get("text", "") for event in events if event["type"] == "token")
    assert answer == "当前知识库未找到依据"
    assert next(event for event in events if event["type"] == "citations")["items"] == []
    assert next(event for event in events if event["type"] == "done")["citations"] == []
