"""Agent 服务请求/响应模型（与后端 service/agent.go 的 HTTP 契约保持一致）。"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    RootModel,
    StringConstraints,
    model_validator,
)


class ParseRequest(BaseModel):
    material_id: int
    parse_generation: int
    content: str
    file_type: str = "txt"
    storage_key: str = ""


class EmbedRequest(BaseModel):
    text: str


class EmbedResponse(BaseModel):
    embedding: list[float]


class ChunkInput(BaseModel):
    team_id: int
    material_id: int
    chapter: str = ""
    chunk_idx: int
    content: str


class ChatHistory(BaseModel):
    role: str
    content: str


class ChatRequest(BaseModel):
    question: str
    session_id: str = ""
    history: list[ChatHistory] = []
    chunks: list[ChunkInput] = []
    service: str = "chat"  # chat | plan | quiz
    deadline: str | None = None
    count: int = 3
    goal: str | None = None
    topic: str | None = None


class PlanRequest(BaseModel):
    goal: str
    deadline: str | None = None
    chunks: list[ChunkInput] = []


class QuizRequest(BaseModel):
    topic: str
    count: int = 3
    chunks: list[ChunkInput] = []


NonEmptyText = Annotated[
    str,
    StringConstraints(strip_whitespace=True, min_length=1, strict=True),
]


class PlanItemResult(BaseModel):
    model_config = ConfigDict(strict=True, extra="forbid")

    date: NonEmptyText
    task: NonEmptyText
    done: bool


class PlanResult(BaseModel):
    model_config = ConfigDict(strict=True, extra="ignore")

    title: NonEmptyText
    goal: NonEmptyText
    items: Annotated[list[PlanItemResult], Field(min_length=1)]


class QuizItemResult(BaseModel):
    model_config = ConfigDict(strict=True, extra="forbid")

    question: NonEmptyText
    options: Annotated[list[NonEmptyText], Field(min_length=2, max_length=4)]
    answer_key: Literal["A", "B", "C", "D"]
    difficulty: NonEmptyText

    @model_validator(mode="after")
    def answer_must_reference_an_option(self) -> QuizItemResult:
        if ord(self.answer_key) - ord("A") >= len(self.options):
            raise ValueError("answer_key does not reference an option")
        return self


class QuizResult(RootModel[Annotated[list[QuizItemResult], Field(min_length=1)]]):
    model_config = ConfigDict(strict=True)
