"""Agent 服务请求/响应模型（与后端 service/agent.go 的 HTTP 契约保持一致）。"""

from __future__ import annotations

from pydantic import BaseModel


class ParseRequest(BaseModel):
    material_id: int
    content: str
    file_type: str = "txt"
    storage_key: str = ""


class RetrieveRequest(BaseModel):
    query: str
    visible_team_ids: list[int]
    only_shared_in_teacher: bool = True
    top_k: int = 5


class ChatHistory(BaseModel):
    role: str
    content: str


class ChatRequest(BaseModel):
    question: str
    session_id: str = ""
    history: list[ChatHistory] = []
    visible_team_ids: list[int] = []
    top_k: int = 5
    service: str = "chat"  # chat | plan | quiz
    material_id: int | None = None
    deadline: str | None = None
    count: int = 3
    goal: str | None = None
    topic: str | None = None


class PlanRequest(BaseModel):
    goal: str
    deadline: str | None = None
    visible_team_ids: list[int] = []


class QuizRequest(BaseModel):
    topic: str
    material_id: int | None = None
    count: int = 3
    visible_team_ids: list[int] = []
