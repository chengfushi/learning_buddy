from __future__ import annotations

import pytest

import rag
from llm import ChunkView


class _RecordingLLM:
    def __init__(self, *, fail: bool = False) -> None:
        self.fail = fail
        self.chunks: list[ChunkView] | None = None

    def chat(
        self,
        _question: str,
        chunks: list[ChunkView],
        _history: object = None,
    ) -> str:
        self.chunks = chunks
        if self.fail:
            raise RuntimeError("injected tutor failure")
        return "fallback-compatible answer"


class _StructuredLLM:
    def __init__(self, result: object) -> None:
        self.result = result

    def plan(self, *_args: object) -> object:
        if isinstance(self.result, Exception):
            raise self.result
        return self.result

    def quiz(self, *_args: object) -> object:
        if isinstance(self.result, Exception):
            raise self.result
        return self.result


@pytest.mark.anyio
async def test_empty_backend_context_refuses_without_calling_tutor(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    llm = _RecordingLLM()
    monkeypatch.setattr(rag, "get_llm", lambda: llm)

    answer, citations = await rag.run_chat_resilient("问题", [], trace_id="trace-empty")

    assert answer == "当前知识库未找到依据"
    assert citations == []
    assert llm.chunks is None


@pytest.mark.anyio
async def test_blank_chunks_do_not_count_as_evidence(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    llm = _RecordingLLM()
    monkeypatch.setattr(rag, "get_llm", lambda: llm)

    answer, citations = await rag.run_chat_resilient(
        "问题", [ChunkView(1, 2, "章节", 0, "  \n\t")], trace_id="trace-blank"
    )

    assert answer == "当前知识库未找到依据"
    assert citations == []
    assert llm.chunks is None


def test_sync_chat_refuses_without_calling_tutor(monkeypatch: pytest.MonkeyPatch) -> None:
    def unexpected_llm() -> object:
        raise AssertionError("tutor must not be called")

    monkeypatch.setattr(rag, "get_llm", unexpected_llm)

    assert rag.run_chat("问题", []) == ("当前知识库未找到依据", [])


@pytest.mark.anyio
async def test_tutor_failure_degrades_to_local_mock(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    chunk = ChunkView(1, 2, "章节", 0, "可见资料内容")
    monkeypatch.setattr(rag, "get_llm", lambda: _RecordingLLM(fail=True))

    answer, citations = await rag.run_chat_resilient("问题", [chunk], trace_id="trace-tutor")

    assert "可见资料内容" in answer
    assert len(citations) == 1


@pytest.mark.parametrize(
    "invalid",
    [
        {"title": "missing goal", "items": []},
        {"title": "title", "goal": "goal", "items": []},
        {
            "title": "title",
            "goal": "goal",
            "items": [{"date": 1, "task": "x", "done": False}],
        },
        ValueError("invalid planner JSON"),
        {
            "title": "   ",
            "goal": "goal",
            "items": [{"date": "D1", "task": "task", "done": False}],
        },
    ],
)
def test_invalid_plan_contract_degrades_to_local_mock(
    monkeypatch: pytest.MonkeyPatch,
    invalid: object,
) -> None:
    monkeypatch.setattr(rag, "get_llm", lambda: _StructuredLLM(invalid))

    result = rag.run_plan("掌握函数", "2026-08-01", [])

    assert result["items"]
    assert result["goal"] == "掌握函数"


@pytest.mark.parametrize(
    "invalid",
    [
        {"questions": []},
        [],
        [{"question": 7, "options": None, "answer_key": "A", "difficulty": "easy"}],
        [{"question": "q", "options": ["a", "b"], "answer_key": "D", "difficulty": "easy"}],
        ValueError("invalid evaluator JSON"),
        [{"question": "\t", "options": ["a", "b"], "answer_key": "A", "difficulty": "easy"}],
    ],
)
def test_invalid_quiz_contract_degrades_to_local_mock(
    monkeypatch: pytest.MonkeyPatch,
    invalid: object,
) -> None:
    monkeypatch.setattr(rag, "get_llm", lambda: _StructuredLLM(invalid))

    result = rag.run_quiz("函数", 3, [])

    assert len(result) == 3
    assert all(
        {"question", "options", "answer_key", "difficulty"} <= item.keys() for item in result
    )
