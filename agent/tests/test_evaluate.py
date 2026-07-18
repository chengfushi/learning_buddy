from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

import evaluate
from schemas import RerankItem, RerankResponse


def evaluation_result(
    *,
    version: str = "rag-v2",
    recall_at_20: float = 0.96,
    rerank_recall_at_5: float = 0.91,
    retrieval_p95_ms: float = 2400,
) -> evaluate.EvaluationResult:
    return {
        "version": version,
        "cases": 100,
        "recall_at_5": 0.94,
        "recall_at_20": recall_at_20,
        "rerank_recall_at_5": rerank_recall_at_5,
        "mrr": 0.90,
        "ndcg_at_5": 0.92,
        "retrieval_p95_ms": retrieval_p95_ms,
        "failed_cases": [],
    }


def test_release_evaluation_rejects_degraded_rerank(monkeypatch: pytest.MonkeyPatch) -> None:
    async def degraded(_request: object) -> RerankResponse:
        return RerankResponse(
            items=[RerankItem(chunk_id=1, score=1.0)],
            model="rrf-order-fallback",
            degraded=True,
        )

    monkeypatch.setattr(evaluate, "rerank", degraded)
    candidates = [evaluate.Candidate(1, 10, "title", "content")]
    with pytest.raises(RuntimeError, match="release evaluation requires"):
        asyncio.run(evaluate._reranked("question", candidates))


def test_release_gate_accepts_metrics_at_or_above_thresholds() -> None:
    failures = evaluate.release_gate_failures(
        [evaluation_result()], "rag-v2", evaluate.ReleaseThresholds()
    )

    assert failures == []


def test_release_gate_reports_every_failed_threshold_and_missing_version() -> None:
    failed = evaluation_result(
        recall_at_20=0.949,
        rerank_recall_at_5=0.899,
        retrieval_p95_ms=2500.1,
    )

    assert evaluate.release_gate_failures([failed], "rag-v2", evaluate.ReleaseThresholds()) == [
        "recall_at_20=0.9490 < 0.9500",
        "rerank_recall_at_5=0.8990 < 0.9000",
        "retrieval_p95_ms=2500.1 > 2500.0",
    ]
    assert evaluate.release_gate_failures([failed], "future-v3", evaluate.ReleaseThresholds()) == [
        "release version 'future-v3' was not evaluated"
    ]


@pytest.mark.parametrize(
    "kwargs",
    [
        {"recall_at_20": float("nan")},
        {"rerank_recall_at_5": 1.01},
        {"retrieval_p95_ms": 0},
    ],
)
def test_release_thresholds_reject_invalid_values(kwargs: dict[str, float]) -> None:
    with pytest.raises(ValueError):
        evaluate.ReleaseThresholds(**kwargs)


def test_release_gate_rejects_non_finite_result() -> None:
    result = evaluation_result(recall_at_20=float("nan"))

    assert evaluate.release_gate_failures([result], "rag-v2", evaluate.ReleaseThresholds()) == [
        "recall_at_20 is not finite"
    ]


@pytest.mark.parametrize(
    ("payload", "message"),
    [
        ('{"question":"","expected_material_ids":[1]}', "non-empty question"),
        ('{"question":"有效问题","expected_material_ids":[]}', "expected_material_ids"),
        ('{"question":"有效问题","expected_material_ids":[true]}', "expected_material_ids"),
        ('{"question":"有效问题","expected_material_ids":[0]}', "expected_material_ids"),
        ("[]", "must be a JSON object"),
        ("\n", "at least one case"),
    ],
)
def test_load_rejects_malformed_cases(tmp_path: Path, payload: str, message: str) -> None:
    case_file = tmp_path / "cases.jsonl"
    case_file.write_text(payload, encoding="utf-8")

    with pytest.raises(ValueError, match=message):
        evaluate._load(case_file, allow_small=True)


def test_evaluate_reports_cases_not_fully_recalled(monkeypatch: pytest.MonkeyPatch) -> None:
    raw = [
        evaluate.Candidate(1, 10, "命中文档", "content"),
        evaluate.Candidate(2, 12, "干扰文档", "content"),
    ]

    monkeypatch.setattr(evaluate, "_retrieve", lambda _question, _version: raw)

    async def fake_reranked(
        _question: str, candidates: list[evaluate.Candidate]
    ) -> list[evaluate.Candidate]:
        return candidates[:1]

    monkeypatch.setattr(evaluate, "_reranked", fake_reranked)
    result = asyncio.run(
        evaluate.evaluate(
            [
                {
                    "id": "mqtt-config",
                    "question": "MQTT 怎么配置？",
                    "expected_material_ids": [10, 11],
                }
            ],
            "rag-v2",
        )
    )

    assert result["recall_at_20"] == 0.5
    assert result["rerank_recall_at_5"] == 0.5
    assert result["failed_cases"] == [
        {
            "case_id": "mqtt-config",
            "expected_material_ids": [10, 11],
            "raw_top_20_material_ids": [10, 12],
            "rerank_top_5_material_ids": [10],
        }
    ]


def test_main_returns_nonzero_when_release_gate_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    case: evaluate.EvaluationCase = {
        "question": "MQTT 怎么配置？",
        "expected_material_ids": [10],
    }
    monkeypatch.setattr(evaluate, "_load", lambda _path, _allow_small: [case])

    async def failed_evaluation(
        _cases: list[evaluate.EvaluationCase], version: str
    ) -> evaluate.EvaluationResult:
        return evaluation_result(version=version, recall_at_20=0.5)

    monkeypatch.setattr(evaluate, "evaluate", failed_evaluation)

    exit_code = asyncio.run(evaluate.main(["cases.jsonl", "--versions", "rag-v2"]))

    captured = capsys.readouterr()
    assert exit_code == 1
    assert '"version": "rag-v2"' in captured.out
    assert "release gate failed" in captured.err
    assert "recall_at_20=0.5000 < 0.9500" in captured.err


def test_main_report_only_does_not_fail_exploration(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    case: evaluate.EvaluationCase = {"question": "问题", "expected_material_ids": [10]}
    monkeypatch.setattr(evaluate, "_load", lambda _path, _allow_small: [case])

    async def failed_evaluation(
        _cases: list[evaluate.EvaluationCase], version: str
    ) -> evaluate.EvaluationResult:
        return evaluation_result(version=version, recall_at_20=0.5)

    monkeypatch.setattr(evaluate, "evaluate", failed_evaluation)

    assert (
        asyncio.run(
            evaluate.main(["cases.jsonl", "--versions", "rag-v2", "--report-only", "--allow-small"])
        )
        == 0
    )


def test_main_rejects_invalid_threshold_before_loading_cases(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        evaluate,
        "_load",
        lambda _path, _allow_small: pytest.fail("cases must not load for invalid thresholds"),
    )

    with pytest.raises(SystemExit) as exc_info:
        asyncio.run(evaluate.main(["cases.jsonl", "--min-recall-at-20", "nan"]))

    assert exc_info.value.code == 2
