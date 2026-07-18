"""离线比较 legacy-v1 / rag-v2；评测数据必须由团队人工标注，不能自动伪造。"""

from __future__ import annotations

import argparse
import asyncio
import json
import math
import re
import statistics
import sys
import time
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import NotRequired, TypedDict

from pgvector import Vector

from db import get_conn
from embed import embed_text
from retrieval import rerank
from schemas import RerankCandidate, RerankRequest


@dataclass
class Candidate:
    chunk_id: int
    material_id: int
    title: str
    content: str
    score: float = 0


@dataclass(frozen=True)
class ReleaseThresholds:
    """发布评测必须达到的最低检索质量和最高延迟。"""

    recall_at_20: float = 0.95
    rerank_recall_at_5: float = 0.90
    retrieval_p95_ms: float = 2500

    def __post_init__(self) -> None:
        for name, value in (
            ("recall_at_20", self.recall_at_20),
            ("rerank_recall_at_5", self.rerank_recall_at_5),
        ):
            if not math.isfinite(value) or not 0 <= value <= 1:
                raise ValueError(f"{name} must be a finite value between 0 and 1")
        if not math.isfinite(self.retrieval_p95_ms) or self.retrieval_p95_ms <= 0:
            raise ValueError("retrieval_p95_ms must be a finite positive value")


class EvaluationCase(TypedDict):
    question: str
    expected_material_ids: list[int]
    id: NotRequired[str | int]


class CaseFailure(TypedDict):
    case_id: str
    expected_material_ids: list[int]
    raw_top_20_material_ids: list[int]
    rerank_top_5_material_ids: list[int]


class EvaluationResult(TypedDict):
    version: str
    cases: int
    recall_at_5: float
    recall_at_20: float
    rerank_recall_at_5: float
    mrr: float
    ndcg_at_5: float
    retrieval_p95_ms: float
    failed_cases: list[CaseFailure]


def _lexical_terms(text: str) -> list[str]:
    terms = [text]
    terms.extend(re.findall(r"[A-Za-z][A-Za-z0-9_.:/-]{1,}|[\u4e00-\u9fff]{2,8}", text))
    return list(dict.fromkeys(term.strip().lower() for term in terms if term.strip()))[:16]


def _load(path: Path, allow_small: bool) -> list[EvaluationCase]:
    rows: list[EvaluationCase] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        raw = json.loads(line)
        if not isinstance(raw, dict):
            raise ValueError(f"case at line {line_number} must be a JSON object")
        question = raw.get("question")
        expected = raw.get("expected_material_ids")
        if not isinstance(question, str) or not question.strip():
            raise ValueError(f"case at line {line_number} needs a non-empty question")
        if (
            not isinstance(expected, list)
            or not expected
            or any(
                not isinstance(value, int) or isinstance(value, bool) or value <= 0
                for value in expected
            )
        ):
            raise ValueError(
                f"case at line {line_number} needs positive integer expected_material_ids"
            )
        row: EvaluationCase = {
            "question": question.strip(),
            "expected_material_ids": expected,
        }
        case_id = raw.get("id")
        if isinstance(case_id, (str, int)) and not isinstance(case_id, bool):
            row["id"] = case_id
        rows.append(row)
    if not rows:
        raise ValueError("evaluation set must contain at least one case")
    if len(rows) < 100 and not allow_small:
        raise ValueError(f"only {len(rows)} cases; at least 100 human-labelled cases are required")
    return rows


def _retrieve(question: str, version: str) -> list[Candidate]:
    embedding = Vector(embed_text(question))
    lexical_terms = _lexical_terms(question)
    ts_query = " OR ".join(lexical_terms)
    with get_conn() as conn, conn.cursor() as cursor:
        cursor.execute(
            """SELECT c.id, c.material_id, m.title, c.content
                 FROM material_chunks c JOIN materials m ON m.id=c.material_id
                WHERE c.index_version=%s ORDER BY c.embedding <=> %s::vector LIMIT 30""",
            (version, embedding),
        )
        vector_rows = cursor.fetchall()
        cursor.execute(
            """SELECT c.id, c.material_id, m.title, c.content
                FROM material_chunks c JOIN materials m ON m.id=c.material_id
                WHERE c.index_version=%s
                  AND (c.lexical_tsv @@ websearch_to_tsquery('simple', %s)
                       OR EXISTS (
                           SELECT 1 FROM unnest(%s::text[]) AS term(value)
                            WHERE c.lexical_text %% term.value
                               OR c.lexical_text ILIKE '%%' || term.value || '%%'
                       ))
                ORDER BY GREATEST(
                         ts_rank_cd(c.lexical_tsv, websearch_to_tsquery('simple', %s)),
                         COALESCE((
                             SELECT MAX(similarity(c.lexical_text, term.value))
                               FROM unnest(%s::text[]) AS term(value)
                         ), 0)) DESC LIMIT 30""",
            (version, ts_query, lexical_terms, ts_query, lexical_terms),
        )
        lexical_rows = cursor.fetchall()
    by_id: dict[int, Candidate] = {}
    for rank, row in enumerate(vector_rows, 1):
        by_id[row[0]] = Candidate(row[0], row[1], row[2], row[3], 1 / (60 + rank))
    for rank, row in enumerate(lexical_rows, 1):
        item = by_id.setdefault(row[0], Candidate(row[0], row[1], row[2], row[3]))
        item.score += 1 / (60 + rank)
    return sorted(by_id.values(), key=lambda item: item.score, reverse=True)[:20]


async def _reranked(question: str, candidates: list[Candidate]) -> list[Candidate]:
    response = await rerank(
        RerankRequest(
            query=question,
            top_n=8,
            candidates=[
                RerankCandidate(
                    chunk_id=item.chunk_id,
                    material_id=item.material_id,
                    title=item.title,
                    content=item.content,
                )
                for item in candidates
            ],
        )
    )
    if response.degraded:
        raise RuntimeError(
            f"rerank degraded to {response.model}; release evaluation requires the configured model"
        )
    by_id = {item.chunk_id: item for item in candidates}
    return [by_id[item.chunk_id] for item in response.items if item.chunk_id in by_id]


def _unique_materials(items: list[Candidate]) -> list[int]:
    return list(dict.fromkeys(item.material_id for item in items))


def _case_metrics(ranking: list[int], expected: set[int], k: int) -> tuple[float, float, float]:
    top = ranking[:k]
    recall = len(expected & set(top)) / len(expected)
    reciprocal_rank = next(
        (1 / rank for rank, item in enumerate(ranking, 1) if item in expected), 0
    )
    dcg = sum((1 / math.log2(rank + 1)) for rank, item in enumerate(top, 1) if item in expected)
    ideal = sum(1 / math.log2(rank + 1) for rank in range(1, min(k, len(expected)) + 1))
    return recall, reciprocal_rank, dcg / ideal if ideal else 0


async def evaluate(cases: list[EvaluationCase], version: str) -> EvaluationResult:
    """评测指定索引版本并返回聚合指标和未完全召回的用例明细。"""

    raw5, raw20, rerank5, mrr, ndcg, latencies = [], [], [], [], [], []
    failed_cases: list[CaseFailure] = []
    for case_number, case in enumerate(cases, 1):
        started = time.perf_counter()
        raw = _retrieve(case["question"], version)
        reranked = await _reranked(case["question"], raw)
        latencies.append((time.perf_counter() - started) * 1000)
        expected = {int(value) for value in case["expected_material_ids"]}
        raw_ranking, rerank_ranking = _unique_materials(raw), _unique_materials(reranked)
        score5 = _case_metrics(raw_ranking, expected, 5)
        score20 = _case_metrics(raw_ranking, expected, 20)
        reranked_score = _case_metrics(rerank_ranking, expected, 5)
        raw5.append(score5[0])
        raw20.append(score20[0])
        rerank5.append(reranked_score[0])
        mrr.append(reranked_score[1])
        ndcg.append(reranked_score[2])
        if score20[0] < 1 or reranked_score[0] < 1:
            failed_cases.append(
                {
                    "case_id": str(case.get("id", case_number)),
                    "expected_material_ids": sorted(expected),
                    "raw_top_20_material_ids": raw_ranking[:20],
                    "rerank_top_5_material_ids": rerank_ranking[:5],
                }
            )
    ordered = sorted(latencies)
    p95 = ordered[max(0, math.ceil(len(ordered) * 0.95) - 1)]
    return {
        "version": version,
        "cases": len(cases),
        "recall_at_5": statistics.fmean(raw5),
        "recall_at_20": statistics.fmean(raw20),
        "rerank_recall_at_5": statistics.fmean(rerank5),
        "mrr": statistics.fmean(mrr),
        "ndcg_at_5": statistics.fmean(ndcg),
        "retrieval_p95_ms": p95,
        "failed_cases": failed_cases,
    }


def release_gate_failures(
    results: Sequence[EvaluationResult],
    release_version: str,
    thresholds: ReleaseThresholds,
) -> list[str]:
    """返回发布门禁失败原因；空列表表示指定版本可以进入灰度。"""

    result = next((item for item in results if item["version"] == release_version), None)
    if result is None:
        return [f"release version {release_version!r} was not evaluated"]

    failures: list[str] = []
    for metric in ("recall_at_20", "rerank_recall_at_5", "retrieval_p95_ms"):
        if not math.isfinite(result[metric]):
            failures.append(f"{metric} is not finite")
    if failures:
        return failures
    if result["recall_at_20"] < thresholds.recall_at_20:
        failures.append(
            f"recall_at_20={result['recall_at_20']:.4f} < {thresholds.recall_at_20:.4f}"
        )
    if result["rerank_recall_at_5"] < thresholds.rerank_recall_at_5:
        failures.append(
            "rerank_recall_at_5="
            f"{result['rerank_recall_at_5']:.4f} < {thresholds.rerank_recall_at_5:.4f}"
        )
    if result["retrieval_p95_ms"] > thresholds.retrieval_p95_ms:
        failures.append(
            f"retrieval_p95_ms={result['retrieval_p95_ms']:.1f} > {thresholds.retrieval_p95_ms:.1f}"
        )
    return failures


async def main(argv: Sequence[str] | None = None) -> int:
    """运行离线评测；发布门禁失败时返回非零退出码。"""

    parser = argparse.ArgumentParser()
    parser.add_argument("cases", type=Path)
    parser.add_argument("--versions", nargs="+", default=["legacy-v1", "rag-v2"])
    parser.add_argument("--allow-small", action="store_true", help="only for local smoke tests")
    parser.add_argument("--release-version", default="rag-v2")
    parser.add_argument("--min-recall-at-20", type=float, default=0.95)
    parser.add_argument("--min-rerank-recall-at-5", type=float, default=0.90)
    parser.add_argument("--max-retrieval-p95-ms", type=float, default=2500)
    parser.add_argument(
        "--report-only",
        action="store_true",
        help="print metrics without failing the release gate; never use for activation",
    )
    args = parser.parse_args(argv)
    try:
        thresholds = ReleaseThresholds(
            recall_at_20=args.min_recall_at_20,
            rerank_recall_at_5=args.min_rerank_recall_at_5,
            retrieval_p95_ms=args.max_retrieval_p95_ms,
        )
    except ValueError as exc:
        parser.error(str(exc))
    cases = _load(args.cases, args.allow_small)
    results = [await evaluate(cases, version) for version in args.versions]
    sys.stdout.write(json.dumps(results, ensure_ascii=False, indent=2) + "\n")
    failures = release_gate_failures(results, args.release_version, thresholds)
    if failures and not args.report_only:
        sys.stderr.write("release gate failed:\n- " + "\n- ".join(failures) + "\n")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
