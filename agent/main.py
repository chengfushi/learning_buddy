"""Agent 服务入口（Python + FastAPI）。

职责：Retriever / Parser / Tutor / Planner / Evaluator 的本地实现。
安全边界：仅持 material_chunks 的检索/解析写凭证；「可见 team 集合」由后端下发，
Agent 不自行拼权限谓词（见 engineering-standards §0 / R2、system-design §7.4）。
"""

from __future__ import annotations

import json
import os
import re

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse

from db import health_ok, settings
from rag import parse, retrieve, run_chat, run_plan, run_quiz
from schemas import ChatRequest, ParseRequest, PlanRequest, QuizRequest, RetrieveRequest

app = FastAPI(title="learning-buddy-agent")

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


@app.post("/parse")
def do_parse(req: ParseRequest) -> dict:
    return parse(req.material_id, req.content, req.file_type, req.storage_key)


@app.post("/retrieve")
def do_retrieve(req: RetrieveRequest) -> dict:
    chunks = retrieve(req.query, req.visible_team_ids, req.only_shared_in_teacher, req.top_k)
    return {
        "chunks": [
            {
                "team_id": c.team_id,
                "material_id": c.material_id,
                "chapter": c.chapter,
                "chunk_idx": c.chunk_idx,
                "content": c.content,
            }
            for c in chunks
        ]
    }


@app.post("/chat")
def do_chat(req: ChatRequest) -> StreamingResponse:
    def generate():
        if req.service == "plan":
            goal = req.goal or req.question
            yield _sse(
                {"type": "result", "payload": run_plan(goal, req.deadline, req.visible_team_ids)}
            )
            yield _sse({"type": "done"})
            return
        if req.service == "quiz":
            topic = req.topic or req.question
            yield _sse(
                {
                    "type": "result",
                    "payload": run_quiz(topic, req.count, req.material_id, req.visible_team_ids),
                }
            )
            yield _sse({"type": "done"})
            return
        answer, citations = run_chat(req.question, req.visible_team_ids, req.top_k, req.history)
        yield _sse({"type": "citations", "items": citations})
        for piece in _tokenize(answer):
            yield _sse({"type": "token", "text": piece})
        yield _sse({"type": "done", "citations": citations})

    return StreamingResponse(generate(), media_type="text/event-stream")


@app.post("/plan")
def do_plan(req: PlanRequest) -> dict:
    return run_plan(req.goal, req.deadline, req.visible_team_ids)


@app.post("/quiz")
def do_quiz(req: QuizRequest) -> list:
    return run_quiz(req.topic, req.count, req.material_id, req.visible_team_ids)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "main:app", host="0.0.0.0", port=int(os.getenv("PORT", settings.port)), reload=False
    )
