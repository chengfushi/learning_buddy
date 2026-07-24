"""可插拔生成层（Tutor / Planner / Evaluator）。

默认使用「确定性降级 mock 生成器」。
若设置 LLM_API_KEY 则走 OpenAI 兼容 Chat Completions。
"""

from __future__ import annotations

import re
from typing import Any, cast

from core.config import settings
from core.http_client import post_sync
from core.utils import redact_for_cloud
from models import ChunkView

NO_EVIDENCE_RESPONSE = "当前知识库未找到依据"


def _first_sentence(text: str, max_len: int = 120) -> str:
    text = (text or "").strip().replace("\n", " ")
    sent = re.split(r"[。.!?！？；;]", text)[0].strip()
    if not sent:
        sent = text[:max_len]
    return sent[:max_len]


class LLM:
    def chat(self, question: str, chunks: list[ChunkView], history: Any = None) -> str:
        raise NotImplementedError

    def plan(self, goal: str, deadline: str | None, chunks: list[ChunkView]) -> dict[str, Any]:
        raise NotImplementedError

    def quiz(self, topic: str, count: int, chunks: list[ChunkView]) -> list[dict[str, Any]]:
        raise NotImplementedError


class MockLLM(LLM):
    """确定性、可复现的 mock 生成器。"""

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
    def _complete(self, system: str, user: str) -> str:
        resp = post_sync(
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
