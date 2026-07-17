"""Agent 服务入口（Python + FastAPI）。

职责：Parser / Tutor / Planner / Evaluator 的本地实现。
安全边界：Backend repository 负责可见性与 pgvector top-k；Agent 生成端只消费
已授权 chunks，Parser 仅持解析所需的最小读写凭证，不查询权限表。
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import uuid
from collections.abc import AsyncIterator

from fastapi import Depends, FastAPI, Header
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse

from auth import assert_agent_auth_config, require_agent_token
from db import assert_embedding_dim, health_ok, settings
from embed import embed_text
from llm import ChunkView
from rag import parse, run_chat_resilient, run_plan, run_quiz
from schemas import ChatRequest, EmbedRequest, EmbedResponse, ParseRequest, PlanRequest, QuizRequest

app = FastAPI(title="learning-buddy-agent")


@app.on_event("startup")
async def startup() -> None:
    assert_agent_auth_config()
    assert_embedding_dim()


app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


def _sse(obj: dict) -> str:
    return "data: " + json.dumps(obj, ensure_ascii=False) + "\n\n"


def _tokenize(text: str):
    parts = re.findall(r"[一-鿿]|[A-Za-z0-9]+|\s+|[^\s]", text or "")
    buf = ""
    for p in parts:
        if re.match(r"[一-鿿]", p):
            buf += p
            if len(buf) >= 3:
                yield buf
                buf = ""
        else:
            if buf:
                yield buf
                buf = ""
            yield p
    if buf:
        yield buf


@app.get("/health")
def health() -> dict:
    return {"status": "ok" if health_ok() else "db_down"}


@app.post("/parse", dependencies=[Depends(require_agent_token)])
def do_parse(req: ParseRequest) -> dict:
    return parse(
        req.material_id,
        req.parse_generation,
        req.content,
        req.file_type,
        req.storage_key,
    )


@app.post("/embed", dependencies=[Depends(require_agent_token)])
def do_embed(req: EmbedRequest) -> EmbedResponse:
    return EmbedResponse(embedding=embed_text(req.text, timeout_s=settings.retriever_timeout_s))


def _chunks(items) -> list[ChunkView]:
    return [
        ChunkView(item.team_id, item.material_id, item.chapter, item.chunk_idx, item.content)
        for item in items
    ]


@app.post("/chat", dependencies=[Depends(require_agent_token)])
async def do_chat(
    req: ChatRequest,
    x_request_id: str | None = Header(default=None, alias="X-Request-ID"),
) -> StreamingResponse:
    trace_id = x_request_id or uuid.uuid4().hex

    async def generate() -> AsyncIterator[str]:
        if req.service == "plan":
            goal = req.goal or req.question
            yield _sse(
                {
                    "type": "result",
                    "payload": await asyncio.to_thread(
                        run_plan, goal, req.deadline, _chunks(req.chunks)
                    ),
                }
            )
            yield _sse({"type": "done"})
            return
        if req.service == "quiz":
            topic = req.topic or req.question
            yield _sse(
                {
                    "type": "result",
                    "payload": await asyncio.to_thread(
                        run_quiz, topic, req.count, _chunks(req.chunks)
                    ),
                }
            )
            yield _sse({"type": "done"})
            return
        answer, citations = await run_chat_resilient(
            req.question,
            _chunks(req.chunks),
            req.history,
            trace_id=trace_id,
        )
        yield _sse({"type": "citations", "items": citations})
        for piece in _tokenize(answer):
            yield _sse({"type": "token", "text": piece})
        yield _sse({"type": "done", "citations": citations})

    return StreamingResponse(
        generate(),
        media_type="text/event-stream",
        headers={"X-Trace-ID": trace_id},
    )


@app.post("/plan", dependencies=[Depends(require_agent_token)])
def do_plan(req: PlanRequest) -> dict:
    return run_plan(req.goal, req.deadline, _chunks(req.chunks))


@app.post("/quiz", dependencies=[Depends(require_agent_token)])
def do_quiz(req: QuizRequest) -> list:
    return run_quiz(req.topic, req.count, _chunks(req.chunks))


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "main:app", host="0.0.0.0", port=int(os.getenv("PORT", settings.port)), reload=False
    )
