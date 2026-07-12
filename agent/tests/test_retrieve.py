"""Agent 检索权限集成测试（R2 反向用例）。

断言：当 teacher team 出现在「后端下发的可见 team 集合」内时，
- only_shared_in_teacher=True：只召回 shared=true 的片段，绝不召回 shared=false 草稿；
- only_shared_in_teacher=False：两者都召回（用于调试/管理员场景）。

注意：team 集合本身由后端授权，Agent 仅在该集合内做 material 级 shared 过滤，
无法扩大可见范围（见 engineering-standards §0 / R2、system-design §7.4）。
"""

from __future__ import annotations

import psycopg2
from pgvector import Vector
from pgvector.psycopg2 import register_vector

from db import settings
from embed import LocalEmbedder
from rag import parse, retrieve

TEAM_ID = 900001
MAT_SHARED_FALSE = 900001
MAT_SHARED_TRUE = 900002
DRAFT = "光合作用是植物利用光能将二氧化碳和水合成有机物的秘密备课草稿"
PUBLIC = "光合作用是绿色植物通过叶绿体将光能转化为化学能的过程，属于 shared 资料"


def _conn():
    conn = psycopg2.connect(settings.pg_dsn, connect_timeout=5)
    register_vector(conn)
    return conn


def setup_module(module):
    emb = LocalEmbedder()
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "INSERT INTO teams (id, name, type, join_code) VALUES (%s,'t-perm','teacher','PERMIT-TEST') "
                "ON CONFLICT (id) DO UPDATE SET type='teacher', join_code='PERMIT-TEST'",
                (TEAM_ID,),
            )
            for mid, shared, content in [
                (MAT_SHARED_FALSE, False, DRAFT),
                (MAT_SHARED_TRUE, True, PUBLIC),
            ]:
                cur.execute(
                    "INSERT INTO materials (id, team_id, title, shared, owner_id) "
                    "VALUES (%s,%s,'perm-mat',%s,1) ON CONFLICT (id) DO UPDATE SET shared=%s",
                    (mid, TEAM_ID, shared, shared),
                )
                cur.execute("DELETE FROM material_chunks WHERE material_id=%s", (mid,))
                cur.execute(
                    "INSERT INTO material_chunks (team_id, material_id, chunk_idx, content, embedding) "
                    "VALUES (%s,%s,0,%s,%s)",
                    (TEAM_ID, mid, content, Vector(emb.embed(content))),
                )
        conn.commit()
    finally:
        conn.close()


def teardown_module(module):
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "DELETE FROM material_chunks WHERE material_id IN (%s,%s)",
                (MAT_SHARED_FALSE, MAT_SHARED_TRUE),
            )
            cur.execute(
                "DELETE FROM materials WHERE id IN (%s,%s)", (MAT_SHARED_FALSE, MAT_SHARED_TRUE)
            )
            cur.execute("DELETE FROM teams WHERE id=%s", (TEAM_ID,))
        conn.commit()
    finally:
        conn.close()


def test_student_cannot_see_shared_false_draft():
    chunks = retrieve("光合作用", [TEAM_ID], only_shared_in_teacher=True, top_k=5)
    contents = [c.content for c in chunks]
    assert all(PUBLIC[:10] in c for c in contents), "应只召回 shared=true 资料"
    assert not any(DRAFT[:10] in c for c in contents), "越权：召回了 teacher 未共享草稿"


def test_admin_sees_both_when_filter_off():
    chunks = retrieve("光合作用", [TEAM_ID], only_shared_in_teacher=False, top_k=5)
    contents = [c.content for c in chunks]
    assert any(DRAFT[:10] in c for c in contents)
    assert any(PUBLIC[:10] in c for c in contents)


def test_empty_visible_set_returns_nothing():
    assert retrieve("光合作用", [], top_k=5) == []


def test_parse_is_idempotent():
    # 两次解析同一资料，chunk 数不应翻倍（R3 幂等）
    material_id = 900003
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute(
                "INSERT INTO teams (id,name,type) VALUES (900002,'t-parse','private') "
                "ON CONFLICT (id) DO NOTHING",
            )
            cur.execute(
                "INSERT INTO materials (id, team_id, title, owner_id) VALUES (%s,900002,'parse-mat',1) "
                "ON CONFLICT (id) DO NOTHING",
                (material_id,),
            )
        conn.commit()
    finally:
        conn.close()

    text = "第一章 向量检索简介。\n\n第二章 权限模型设计。\n\n第三章 解析流水线实现。"
    parse(material_id, text, "txt", "k1")
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute("SELECT count(*) FROM material_chunks WHERE material_id=%s", (material_id,))
            n1 = cur.fetchone()[0]
    finally:
        conn.close()

    parse(material_id, text, "txt", "k2")  # 重复触发（幂等）
    conn = _conn()
    try:
        with conn.cursor() as cur:
            cur.execute("SELECT count(*) FROM material_chunks WHERE material_id=%s", (material_id,))
            n2 = cur.fetchone()[0]
            cur.execute("SELECT parse_status FROM materials WHERE id=%s", (material_id,))
            status = cur.fetchone()[0]
        assert n1 == n2, f"幂等失败：首次 {n1} ≠ 重复 {n2}"
        assert n1 > 0, "解析未生成任何 chunk"
        assert status == "done"
    finally:
        with conn.cursor() as cur:
            cur.execute("DELETE FROM material_chunks WHERE material_id=%s", (material_id,))
            cur.execute("DELETE FROM materials WHERE id=%s", (material_id,))
            cur.execute("DELETE FROM teams WHERE id=900002")
        conn.commit()
        conn.close()
