"""离线比较 legacy-v1 / rag-v2；评测数据必须由团队人工标注，不能自动伪造。"""

from __future__ import annotations

import argparse
import asyncio
import json
import math
import re
import statistics
import time
from dataclasses import dataclass
from pathlib import Path

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


def _lexical_terms(text: str) -> list[str]:
    terms = [text]
    terms.extend(re.findall(r"[A-Za-z][A-Za-z0-9_.:/-]{1,}|[\u4e00-\u9fff]{2,8}", text))
    return list(dict.fromkeys(term.strip().lower() for term in terms if term.strip()))[:16]


def _load(path: Path, allow_small: bool) -> list[dict]:
    rows = [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line]
    for row in rows:
        if not row.get("question") or not row.get("expected_material_ids"):
            raise ValueError("each case needs question and expected_material_ids")
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


async def evaluate(cases: list[dict], version: str) -> dict:
    raw5, raw20, rerank5, mrr, ndcg, latencies = [], [], [], [], [], []
    for case in cases:
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
    }


async def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("cases", type=Path)
    parser.add_argument("--versions", nargs="+", default=["legacy-v1", "rag-v2"])
    parser.add_argument("--allow-small", action="store_true", help="only for local smoke tests")
    args = parser.parse_args()
    cases = _load(args.cases, args.allow_small)
    results = [await evaluate(cases, version) for version in args.versions]
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    asyncio.run(main())
