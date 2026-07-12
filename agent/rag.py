"""RAG 检索 / 解析 / 编排（Retriever + Parser + Tutor/Planner/Evaluator）。

权限边界（engineering-standards §0 / R2，system-design §7.4）：
- 可见 team 集合由后端 repository 计算并通过请求下发，Agent 不自行判定成员/权限。
- 检索谓词 `team_id IN(可见集) AND (type<>'teacher' OR shared)` 与后端
  repository.VisibleMaterialsScope 完全一致；team 集合由后端授权，Agent 只能在
  该集合内做 material 级 shared  refinement，无法扩大可见范围 → 学生读不到
  teacher 未共享草稿（R2 反向测试覆盖）。
"""

from __future__ import annotations

import re

from pgvector import Vector

from db import get_conn
from embed import get_embedder
from llm import ChunkView, get_llm

_embedder = get_embedder()


def retrieve(
    query: str,
    visible_team_ids: list[int] | None,
    only_shared_in_teacher: bool = True,
    top_k: int = 5,
) -> list[ChunkView]:
    """在后端授权的可见 team 集合内做向量检索（余弦最近邻）。"""
    visible = [int(t) for t in (visible_team_ids or [])]
    if not visible:
        return []
    top_k = max(1, min(top_k or 5, 50))
    q_emb = _embedder.embed(query)

    sql = """
        SELECT c.team_id, c.material_id, m.chapter, c.chunk_idx, c.content,
               1 - (c.embedding <=> %s) AS score
        FROM material_chunks c
        JOIN materials m ON m.id = c.material_id
        JOIN teams t ON t.id = m.team_id
        WHERE m.team_id = ANY(%s)
          AND (%s OR t.type <> 'teacher' OR m.shared = true)
        ORDER BY c.embedding <=> %s
        LIMIT %s
    """
    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute(
                sql,
                (Vector(q_emb), visible, not only_shared_in_teacher, Vector(q_emb), top_k),
            )
            rows = cur.fetchall()
    return [
        ChunkView(team_id=r[0], material_id=r[1], chapter=r[2] or "", chunk_idx=r[3], content=r[4])
        for r in rows
    ]


def _chunk_text(text: str, size: int = 600, overlap: int = 80) -> list[str]:
    text = text or ""
    paras = [p.strip() for p in re.split(r"\n\s*\n", text) if p.strip()]
    if not paras:
        paras = [text.strip()]
    out: list[str] = []
    buf = ""
    for p in paras:
        if buf and len(buf) + len(p) > size:
            out.append(buf)
            buf = (buf[-overlap:] + "\n" + p) if overlap else p
        else:
            buf = (buf + "\n" + p).strip()
    if buf:
        out.append(buf)
    final: list[str] = []
    for c in out:
        if len(c) <= size:
            final.append(c)
        else:
            for i in range(0, max(1, len(c) - size), max(1, size - overlap)):
                final.append(c[i : i + size])
    return [c for c in final if c.strip()]


def parse(material_id: int, content: str, file_type: str, storage_key: str) -> dict:
    """Parser 任务：切分 → 嵌入 → 幂等写 chunks → 回填 materials（R3 幂等）。"""
    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT team_id FROM materials WHERE id = %s", (material_id,))
            row = cur.fetchone()
            if not row:
                raise ValueError(f"material {material_id} not found")
            team_id = row[0]

            chunks = _chunk_text(content)
            # 幂等：重解析先删旧 chunk，避免重复（R3）
            cur.execute("DELETE FROM material_chunks WHERE material_id = %s", (material_id,))
            for idx, piece in enumerate(chunks):
                emb = _embedder.embed(piece)
                cur.execute(
                    """INSERT INTO material_chunks (team_id, material_id, chunk_idx, content, embedding)
                       VALUES (%s, %s, %s, %s, %s)""",
                    (team_id, material_id, idx, piece, Vector(emb)),
                )
            cur.execute(
                """UPDATE materials
                   SET content = %s, parse_status = 'done', parse_error = NULL
                   WHERE id = %s""",
                (content, material_id),
            )
    return {"material_id": material_id, "chunks": len(chunks), "status": "done"}


def run_chat(question: str, visible_team_ids, top_k: int, history=None) -> tuple[str, list[dict]]:
    chunks = retrieve(question, visible_team_ids, True, top_k)
    answer = get_llm().chat(question, chunks, history)
    citations = [
        {
            "team_id": c.team_id,
            "material_id": c.material_id,
            "chapter": c.chapter,
            "chunk_idx": c.chunk_idx,
            "snippet": c.content[:120],
        }
        for c in chunks
    ]
    return answer, citations


def run_plan(goal: str, deadline: str | None, visible_team_ids) -> dict:
    chunks = retrieve(goal, visible_team_ids, True, 5)
    return get_llm().plan(goal, deadline, chunks)


def run_quiz(topic: str, count: int, material_id: int | None, visible_team_ids) -> list[dict]:
    if material_id:
        # 限定在该资料内检索
        chunks = _retrieve_in_material(topic, material_id, count + 2)
    else:
        chunks = retrieve(topic, visible_team_ids, True, count + 2)
    return get_llm().quiz(topic, count, chunks)


def _retrieve_in_material(query: str, material_id: int, top_k: int) -> list[ChunkView]:
    q_emb = _embedder.embed(query)
    sql = """
        SELECT c.team_id, c.material_id, m.chapter, c.chunk_idx, c.content
        FROM material_chunks c
        JOIN materials m ON m.id = c.material_id
        WHERE c.material_id = %s
        ORDER BY c.embedding <=> %s
        LIMIT %s
    """
    with get_conn() as conn:
        with conn.cursor() as cur:
            cur.execute(sql, (material_id, Vector(q_emb), top_k))
            rows = cur.fetchall()
    return [ChunkView(r[0], r[1], r[2] or "", r[3], r[4]) for r in rows]
