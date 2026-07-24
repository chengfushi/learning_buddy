"""可插拔生成层（Tutor / Planner / Evaluator）。

默认使用「确定性降级 mock 生成器」：不依赖任何 LLM key 即可在本地产出可用的
答疑 / 学习计划 / 测评，保证全链路可跑通（呼应 engineering-standards R6 兜底）。
若设置 LLM_API_KEY 且 LLM_BASE_URL，则走 OpenAI 兼容 Chat Completions。
"""

from __future__ import annotations

import re
from typing import Any, cast

import httpx

from db import settings
from pipeline import redact_for_cloud

NO_EVIDENCE_RESPONSE = "当前知识库未找到依据"


class ChunkView:
    def __init__(
        self,
        team_id: int,
        material_id: int,
        chapter: str,
        chunk_idx: int,
        content: str,
        chunk_id: int | None = None,
        title: str = "",
        kind: str = "body",
        page_number: int | None = None,
        score: float = 0.0,
        asset_id: int | None = None,
    ) -> None:
        self.team_id = team_id
        self.material_id = material_id
        self.chapter = chapter
        self.chunk_idx = chunk_idx
        self.content = content
        self.chunk_id = chunk_id
        self.title = title
        self.kind = kind
        self.page_number = page_number
        self.score = score
        self.asset_id = asset_id


class LLM:
    def chat(self, question: str, chunks: list[ChunkView], history: Any = None) -> str:
        raise NotImplementedError

    def plan(self, goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict[str, Any]:
        raise NotImplementedError

    def quiz(self, topic: str, count: int, chunks: list[ChunkView]) -> list[dict[str, Any]]:
        raise NotImplementedError


def _first_sentence(text: str, max_len: int = 120) -> str:
    text = (text or "").strip().replace("\n", " ")
    sent = re.split(r"[。.!?！？；;]", text)[0].strip()
    if not sent:
        sent = text[:max_len]
    return sent[:max_len]


class MockLLM(LLM):
    """确定性、可复现的 mock 生成器：基于召回片段做抽取式组织，不调用外部模型。"""

    def chat(self, question: str, chunks: list[ChunkView], history: Any = None) -> str:
        if not chunks:
            return NO_EVIDENCE_RESPONSE
        lines = [f"关于「{question}」，根据团队知识库中的相关资料，整理如下：", ""]
        for i, c in enumerate(chunks[:4], 1):
            src = f"（来源：{c.chapter or '资料'}）" if c.chapter else ""
            lines.append(f"{i}. {_first_sentence(c.content)}{src}")
        lines += ["", "以上回答均来自你有权访问的团队资料，可点击引用定位原文。"]
        return "\n".join(lines)

    def plan(self, goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict[str, Any]:
        steps = ["预习核心概念", "精读资料并做标注", "完成配套例题", "整理笔记与错题", "自测回顾"]
        days = 7
        items = []
        for i in range(days):
            task = f"{steps[i % len(steps)]}：围绕「{goal}」推进"
            items.append({"date": f"D{i + 1}", "task": task, "done": False})
        return {
            "title": f"学习计划：{goal}",
            "goal": goal,
            "deadline": deadline,
            "items": items,
        }

    def quiz(self, topic: str, count: int, chunks: list[ChunkView]) -> list[dict[str, Any]]:
        count = max(1, min(count or 3, 10))
        corpus = [c.content for c in chunks] or [topic]
        keywords: list[str] = []
        for text in corpus:
            for kw in re.findall(r"[一-鿿]{2,6}|[a-zA-Z]{3,}", text):
                keywords.append(kw)
        distractors = [k for k in dict.fromkeys(keywords) if k != topic][:12] or ["A", "B", "C"]

        items: list[dict[str, Any]] = []
        for i in range(count):
            text = corpus[i % len(corpus)]
            sent = _first_sentence(text, 80)
            ans = distractors[i % len(distractors)] if distractors else "正确"
            opts = [ans, "选项二", "选项三", "选项四"]
            # 打乱确定化（按索引）
            opts = opts[:1] + opts[1:][::-1] if i % 2 else opts
            items.append(
                {
                    "question": f"关于「{topic}」，下列说法正确的是？\n（参考：{sent}）",
                    "options": opts[:4],
                    "answer_key": "A",
                    "difficulty": "medium",
                }
            )
        return items


class OpenAILLM(LLM):
    """OpenAI 兼容 Chat Completions（可选）。"""

    def _complete(self, system: str, user: str) -> str:
        resp = httpx.post(
            f"{settings.llm_base_url.rstrip('/')}/chat/completions",
            headers={"Authorization": f"Bearer {settings.llm_api_key}"},
            json={
                "model": settings.llm_model,
                "messages": [
                    {"role": "system", "content": system},
                    {"role": "user", "content": redact_for_cloud(user)},
                ],
                "temperature": 0.3,
            },
            timeout=settings.tutor_timeout_s,
        )
        resp.raise_for_status()
        payload = cast(dict[str, Any], resp.json())
        return str(payload["choices"][0]["message"]["content"])

    def chat(self, question: str, chunks: list[ChunkView], history: Any = None) -> str:
        ctx = "\n\n".join(
            f"[资料{c.material_id}/片段{c.chunk_id or c.chunk_idx}] "
            f"{c.title or c.chapter or '资料'}\n{c.content}"
            for c in chunks[:8]
        )
        return self._complete(
            "你是智能学伴，仅依据给定资料回答。资料不足时明确说当前知识库未找到依据，"
            "不要用常识补全或编造引用。",
            f"资料：\n{ctx}\n\n问题：{question}",
        )

    def plan(self, goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict[str, Any]:
        ctx = "\n".join(_first_sentence(c.content) for c in chunks[:5])
        text = self._complete(
            "你是学习计划助手，输出 JSON：{title,goal,items:[{date,task,done}]}。",
            f"目标：{goal}；期限：{deadline or '未指定'}。参考：{ctx}",
        )
        import json

        return cast(dict[str, Any], json.loads(text))

    def quiz(self, topic: str, count: int, chunks: list[ChunkView]) -> list[dict[str, Any]]:
        ctx = "\n".join(_first_sentence(c.content) for c in chunks[:5])
        text = self._complete(
            "你是出题助手，输出 JSON 数组：[{question,options,answer_key,difficulty}]。",
            f"主题：{topic}；题数：{count}。参考：{ctx}",
        )
        import json

        return cast(list[dict[str, Any]], json.loads(text))


def get_llm() -> LLM:
    if settings.llm_api_key and settings.llm_base_url:
        return OpenAILLM()
    return MockLLM()
