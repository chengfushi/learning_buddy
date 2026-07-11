"""Agent 服务入口（Python + Google ADK + A2A）。

职责：多智能体编排（Orchestrator/Parser/Retriever/Tutor/Planner/Evaluator/Memory）。
安全边界：仅持 material_chunks 的**只读**凭证；检索谓词（可见 team 集合 + shared 过滤）
由后端通过请求注入，Agent 不直接访问权限表或全库。
"""

from __future__ import annotations

import os

from fastapi import FastAPI
from pydantic import BaseModel
from pydantic_settings import BaseSettings, SettingsConfigDict

app = FastAPI(title="learning-buddy-agent")


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    pg_dsn: str = "postgres://learning_ro:learning@localhost:5432/learning_buddy"
    redis_addr: str = "localhost:6379"
    embedding_dim: int = 768  # 全库必须一致（见 engineering-standards R1）


settings = Settings()


class RetrieveRequest(BaseModel):
    query: str
    visible_team_ids: list[int]
    # 后端下发的过滤谓词片段：teacher team 仅取 shared=true
    only_shared_in_teacher: bool = True
    top_k: int = 5


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/retrieve")
def retrieve(req: RetrieveRequest) -> dict[str, object]:
    """Retriever 智能体：在后端下发的可见 team 集合内做向量检索。

    注意：谓词中的 `shared` 过滤由后端保证，Agent 只消费结果，不自行拼权限 SQL。
    """
    # TODO: 接入 pgvector 向量检索（仅读 material_chunks，受 visible_team_ids 约束）
    return {"chunks": [], "note": "placeholder"}


@app.post("/chat")
async def chat(question: str) -> dict[str, object]:
    """Orchestrator 入口占位：后续编排 Retriever -> Tutor，SSE 流式回传。"""
    return {"answer": "placeholder", "question": question}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=int(os.getenv("PORT", "8000")),
        reload=False,
    )
