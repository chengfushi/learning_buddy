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


class AssetResult(BaseModel):
    id: int
    page_number: int | None = None
    storage_key: str
    caption: str = ""
    ocr_text: str = ""


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
    chunk_id: int | None = None
    title: str = ""
    kind: str = "body"
    page_number: int | None = None
    score: float = 0.0
    asset_id: int | None = None


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
    retrieval_query: str = ""


class QueryAnalysisRequest(BaseModel):
    question: str = Field(min_length=1)
    history: list[ChatHistory] = []


class QueryAnalysisResponse(BaseModel):
    retrieval_query: str = Field(min_length=1)
    keywords: list[str] = []
    embedding: list[float] = []
    rewrite_applied: bool = False
    model: str = "local"


class RerankCandidate(BaseModel):
    chunk_id: int
    material_id: int
    content: str
    title: str = ""
    kind: str = "body"


class RerankRequest(BaseModel):
    query: str = Field(min_length=1)
    candidates: Annotated[list[RerankCandidate], Field(max_length=20)] = []
    top_n: int = Field(default=8, ge=1, le=20)


class RerankItem(BaseModel):
    chunk_id: int
    score: float


class RerankResponse(BaseModel):
    items: list[RerankItem]
    model: str
    degraded: bool = False


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
