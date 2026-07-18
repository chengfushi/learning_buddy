import { z } from "zod";

// 与后端 REST 契约对齐（后端 GORM 模型无 json tag，按 Go 字段名序列化）。
// 严格类型，禁用 any（eslint: no-explicit-any）。

export interface User {
  ID: number;
  Email: string;
  DisplayName: string;
  Role: string;
  Subscription: string;
  CreatedAt: string;
}

export interface Team {
  ID: number;
  Name: string;
  Type: string;
  JoinCode: string | null;
  OwnerID: number | null;
  CreatedAt: string;
}

export interface Material {
  ID: number;
  TeamID: number;
  Title: string;
  Subject: string | null;
  Chapter: string | null;
  Tags: string[] | null;
  Content: string | null;
  FileType: string | null;
  ParseStatus: string;
  ParseGeneration: number;
  ParseError: string | null;
  Shared: boolean;
  OwnerID: number;
  CreatedAt: string;
  Summary?: string | null;
  SemanticKeywords?: string[] | null;
  SuggestedQuestions?: string[] | null;
  NormalizedStorageKey?: string | null;
  IndexVersion?: string;
}

const MaterialSchema: z.ZodType<Material> = z.object({
  ID: z.number(),
  TeamID: z.number(),
  Title: z.string(),
  Subject: z.string().nullable(),
  Chapter: z.string().nullable(),
  Tags: z.array(z.string()).nullable(),
  Content: z.string().nullable(),
  FileType: z.string().nullable(),
  ParseStatus: z.string(),
  ParseGeneration: z.number(),
  ParseError: z.string().nullable(),
  Shared: z.boolean(),
  OwnerID: z.number(),
  CreatedAt: z.string(),
  Summary: z.string().nullable().optional(),
  SemanticKeywords: z.array(z.string()).nullable().optional(),
  SuggestedQuestions: z.array(z.string()).nullable().optional(),
  NormalizedStorageKey: z.string().nullable().optional(),
  IndexVersion: z.string().optional(),
});

const TeamMaterialsResponseSchema = z.object({
  materials: z.array(MaterialSchema),
  can_write: z.boolean(),
});

const RetryMaterialResponseSchema = z.object({ material: MaterialSchema });

export interface TeamMember {
  TeamID: number;
  UserID: number;
  Role: string;
  Status: string;
  JoinedAt: string;
}

export interface MaterialNote {
  ID: number;
  UserID: number;
  MaterialID: number;
  Content: string;
  Quote: string | null;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface LearningRecord {
  ID: number;
  UserID: number;
  MaterialID: number | null;
  DurationS: number;
  Progress: number;
  Score: number | null;
  CreatedAt: string;
}

export interface DailyProgress {
  date: string;
  duration_s: number;
  avg_progress: number;
}

export interface ProgressSummary {
  total_duration_s: number;
  avg_progress: number;
  quiz_count: number;
  quiz_correct: number;
  quiz_accuracy: number;
  daily: DailyProgress[];
}

export interface PlanItem {
  date: string;
  task: string;
  done: boolean;
}

export interface StudyPlan {
  ID: number;
  UserID: number;
  Title: string;
  Goal: string | null;
  Deadline: string | null;
  Items: PlanItem[] | null;
  CreatedAt: string;
}

export interface Exercise {
  ID: number;
  MaterialID: number | null;
  Question: string;
  Options: string[] | null;
  Difficulty: string | null;
  CreatedAt: string;
}

export interface AgentSession {
  ID: string;
  UserID: number;
  MaterialID: number | null;
  Title: string | null;
  CreatedAt: string;
}

export interface AgentMessage {
  ID: number;
  Role: "user" | "assistant" | "system";
  Content: string;
  Citations: Citation[];
  CreatedAt: string;
}

export interface Citation {
  team_id: number;
  material_id: number;
  chapter: string;
  chunk_idx: number;
  chunk_id?: number;
  title?: string;
  snippet?: string;
  kind?: string;
  page_number?: number;
  score?: number;
  asset_id?: number;
}

const CitationSchema: z.ZodType<Citation> = z.object({
  team_id: z.number().int(),
  material_id: z.number().int().positive(),
  chapter: z.string(),
  chunk_idx: z.number().int().nonnegative(),
  chunk_id: z.number().int().positive().optional(),
  title: z.string().optional(),
  snippet: z.string().optional(),
  kind: z.string().optional(),
  page_number: z.number().int().positive().optional(),
  score: z.number().optional(),
  asset_id: z.number().int().positive().optional(),
});

const AgentSessionSchema: z.ZodType<AgentSession> = z.object({
  ID: z.string().min(1),
  UserID: z.number().int().positive(),
  MaterialID: z.number().int().positive().nullable(),
  Title: z.string().nullable(),
  CreatedAt: z.string(),
});

const AgentMessageSchema: z.ZodType<AgentMessage> = z.object({
  ID: z.number().int().positive(),
  Role: z.enum(["user", "assistant", "system"]),
  Content: z.string(),
  Citations: z.array(CitationSchema),
  CreatedAt: z.string(),
});

const AgentSessionsResponseSchema = z.object({ sessions: z.array(AgentSessionSchema) });
const AgentSessionResponseSchema = z.object({
  session_id: z.string().min(1),
  messages: z.array(AgentMessageSchema),
});

const ChatEventSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("token"), text: z.string() }),
  z.object({
    type: z.literal("meta"),
    session_id: z.string().min(1),
    trace_id: z.string().default(""),
    rewritten_query: z.string().default(""),
    rewrite_applied: z.boolean().default(false),
  }),
  z.object({
    type: z.literal("done"),
    session_id: z.string().min(1),
    message_id: z.number().int().nonnegative().default(0),
    citations: z.array(CitationSchema).default([]),
    stage_ms: z.record(z.number().nonnegative()).default({}),
    degraded_stages: z.array(z.string()).default([]),
  }),
  z.object({
    type: z.literal("error"),
    text: z.string().default(""),
    message: z.string().default(""),
  }),
  z.object({ type: z.literal("end") }),
]);

export interface MaterialAsset {
  id: number;
  page_number: number | null;
  caption: string | null;
  ocr_text: string | null;
  url: string;
}

export interface MaterialProcessing {
  ID: number;
  MaterialID: number;
  ParseGeneration: number;
  IndexVersion: string;
  Stage: string;
  Status: string;
  Progress: Record<string, number | string>;
}

export interface ChatMeta {
  session_id: string;
  trace_id: string;
  rewritten_query: string;
  rewrite_applied: boolean;
}

export interface ChatDone {
  session_id: string;
  message_id: number;
  citations: Citation[];
  stage_ms: Record<string, number>;
  degraded_stages: string[];
}

export interface ChatResult {
  answer: string;
  citations: Citation[];
}

const TOKEN_KEY = "lb_token";

const MaterialAssetSchema: z.ZodType<MaterialAsset> = z.object({
  id: z.number(),
  page_number: z.number().nullable(),
  caption: z.string().nullable(),
  ocr_text: z.string().nullable(),
  url: z.string(),
});

const MaterialAssetsResponseSchema = z.object({ assets: z.array(MaterialAssetSchema) });
const SourceURLResponseSchema = z.object({ url: z.string().url(), expires_in: z.number() });
const MaterialProcessingSchema: z.ZodType<MaterialProcessing> = z.object({
  ID: z.number(),
  MaterialID: z.number(),
  ParseGeneration: z.number(),
  IndexVersion: z.string(),
  Stage: z.string(),
  Status: z.string(),
  Progress: z.record(z.union([z.number(), z.string()])),
});

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}
export function setToken(t: string): void {
  localStorage.setItem(TOKEN_KEY, t);
}
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  schema?: z.ZodType<T>,
): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(`/api${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    let msg = `请求失败 (${res.status})`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j && j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  const text = await res.text();
  const payload: unknown = text ? JSON.parse(text) : {};
  return schema ? schema.parse(payload) : (payload as T);
}

async function streamPost(
  path: string,
  body: unknown,
  onLine: (line: string) => void,
): Promise<void> {
  const token = getToken();
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const res = await fetch(`/api${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (!res.ok || !res.body) {
    let msg = `请求失败 (${res.status})`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j && j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.startsWith("data:")) onLine(trimmed.slice(5).trim());
    }
  }
  buffer += decoder.decode();
  const trailing = buffer.trim();
  if (trailing.startsWith("data:")) onLine(trailing.slice(5).trim());
}

export const api = {
  // 账号
  register: (email: string, password: string, displayName: string, role: string) =>
    request<{ user: User; access_token: string; refresh_token: string }>("POST", "/auth/register", {
      email,
      password,
      display_name: displayName,
      role,
    }),
  login: (email: string, password: string) =>
    request<{ user: User; access_token: string; refresh_token: string }>("POST", "/auth/login", {
      email,
      password,
    }),
  me: () => request<{ user: User }>("GET", "/me"),

  // 团队
  listTeams: () => request<{ teams: Team[] }>("GET", "/teams"),
  createTeam: (name: string) => request<Team>("POST", "/teams", { name }),
  joinTeam: (id: number) =>
    request<{ status: string; team_id: number }>("POST", `/teams/${id}/join`),
  joinByCode: (code: string) =>
    request<{ status: string; team_id: number }>("POST", "/teams/join", { code }),
  listMembers: (id: number) => request<{ members: TeamMember[] }>("GET", `/teams/${id}/members`),
  approveMember: (id: number, uid: number) =>
    request<{ ok: boolean }>("POST", `/teams/${id}/members/${uid}/approve`),
  listTeamMaterials: (id: number) =>
    request("GET", `/teams/${id}/materials`, undefined, TeamMaterialsResponseSchema),

  // 资料
  listMaterials: (teamId?: number, q?: string) => {
    const qs: string[] = [];
    if (teamId) qs.push(`team_id=${teamId}`);
    if (q) qs.push(`q=${encodeURIComponent(q)}`);
    return request<{ materials: Material[] }>(
      "GET",
      `/materials${qs.length ? `?${qs.join("&")}` : ""}`,
    );
  },
  createMaterial: (input: {
    team_id: number;
    title: string;
    subject?: string;
    chapter?: string;
    content?: string;
    file_type?: string;
    tags?: string[];
  }) => request<{ material: Material }>("POST", "/materials", input),
  uploadMaterial: async (input: { team_id: number; title: string; file: File }) => {
    const form = new FormData();
    form.set("team_id", String(input.team_id));
    form.set("title", input.title);
    form.set("file", input.file);
    const headers: Record<string, string> = {};
    const token = getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    const response = await fetch("/api/materials", { method: "POST", headers, body: form });
    if (!response.ok) {
      const payload = (await response.json().catch(() => ({}))) as { error?: string };
      throw new ApiError(response.status, payload.error ?? `上传失败 (${response.status})`);
    }
    const payload: unknown = await response.json();
    return z.object({ material: MaterialSchema }).parse(payload);
  },
  getMaterial: (id: number) => request<{ material: Material }>("GET", `/materials/${id}`),
  updateMaterial: (id: number, body: { title?: string; content?: string; shared?: boolean }) =>
    request<{ material: Material }>("PUT", `/materials/${id}`, body),
  deleteMaterial: (id: number) => request<{ ok: boolean }>("DELETE", `/materials/${id}`),
  retryMaterialParse: (id: number) =>
    request("POST", `/materials/${id}/retry`, undefined, RetryMaterialResponseSchema),
  getMaterialSourceURL: (id: number) =>
    request("GET", `/materials/${id}/source-url`, undefined, SourceURLResponseSchema),
  listMaterialAssets: (id: number) =>
    request("GET", `/materials/${id}/assets`, undefined, MaterialAssetsResponseSchema),
  getMaterialProcessing: (id: number) =>
    request(
      "GET",
      `/materials/${id}/processing`,
      undefined,
      z.object({ processing: MaterialProcessingSchema.nullable() }),
    ),

  // 笔记
  listNotes: (id: number) => request<{ notes: MaterialNote[] }>("GET", `/materials/${id}/notes`),
  createNote: (id: number, content: string, quote?: string) =>
    request<{ note: MaterialNote }>("POST", `/materials/${id}/notes`, { content, quote }),

  // 学习记录 / 进度
  createLearningRecord: (input: {
    material_id?: number;
    duration_s: number;
    progress: number;
    score?: number;
  }) => request<{ record: LearningRecord }>("POST", "/learning/records", input),
  listLearningRecords: () => request<{ records: LearningRecord[] }>("GET", "/learning/records"),
  getProgress: () => request<{ summary: ProgressSummary }>("GET", "/learning/progress"),

  // 计划 / 测评 / 对话
  createPlan: (goal: string, deadline?: string, title?: string) =>
    request<{ plan: StudyPlan }>("POST", "/agent/plan", { goal, deadline, title }),
  createQuiz: (topic: string, count: number, materialId?: number) =>
    request<{ exercises: Exercise[] }>("POST", "/agent/quiz", {
      topic,
      count,
      material_id: materialId,
    }),
  answerQuiz: (id: number, choice: string) =>
    request<{ is_correct: boolean; correct_key: string | null; attempt: unknown }>(
      "POST",
      `/agent/quiz/${id}/answer`,
      { choice },
    ),
  listSessions: () => request("GET", "/agent/sessions", undefined, AgentSessionsResponseSchema),
  getSession: (sessionId: string) =>
    request(
      "GET",
      `/agent/sessions/${encodeURIComponent(sessionId)}`,
      undefined,
      AgentSessionResponseSchema,
    ),
  submitFeedback: (messageId: number, rating: "up" | "down", reason?: string) =>
    request<{ feedback: { ID: number; Rating: string; Reason: string | null } }>(
      "PUT",
      `/agent/messages/${messageId}/feedback`,
      { rating, reason },
    ),

  // SSE 答疑（fetch + ReadableStream，避免 token 进 URL，呼应 R4）
  chatStream: async (
    req: { question: string; session_id?: string; material_id?: number },
    handlers: {
      onToken: (t: string) => void;
      /** @deprecated citations are carried by onDone; kept for existing clients. */
      onCitations?: (citations: Citation[]) => void;
      onMeta?: (meta: ChatMeta) => void;
      onDone?: (done: ChatDone) => void;
      onEnd: () => void;
      onError: (m: string) => void;
    },
  ): Promise<void> => {
    try {
      await streamPost("/agent/chat", req, (payload) => {
        if (!payload) return;
        try {
          const parsed = ChatEventSchema.safeParse(JSON.parse(payload) as unknown);
          if (!parsed.success) return;
          const ev = parsed.data;
          if (ev.type === "token" && ev.text) handlers.onToken(ev.text);
          else if (ev.type === "meta") {
            handlers.onMeta?.({
              session_id: ev.session_id,
              trace_id: ev.trace_id,
              rewritten_query: ev.rewritten_query,
              rewrite_applied: ev.rewrite_applied,
            });
          } else if (ev.type === "done") {
            handlers.onCitations?.(ev.citations);
            handlers.onDone?.({
              session_id: ev.session_id,
              message_id: ev.message_id,
              citations: ev.citations,
              stage_ms: ev.stage_ms,
              degraded_stages: ev.degraded_stages,
            });
          } else if (ev.type === "error") handlers.onError(ev.text || ev.message || "对话请求失败");
        } catch {
          /* 忽略无法解析的行 */
        }
      });
      handlers.onEnd();
    } catch (e) {
      handlers.onError(e instanceof Error ? e.message : "对话请求失败");
    }
  },
};

export type { ApiError };
