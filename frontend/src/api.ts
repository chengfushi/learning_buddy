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
  Shared: boolean;
  OwnerID: number;
  CreatedAt: string;
}

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
  AnswerKey: string | null;
  Difficulty: string | null;
  CreatedAt: string;
}

export interface AgentSession {
  ID: string;
  UserID: number;
  Title: string | null;
  CreatedAt: string;
}

export interface Citation {
  team_id: number;
  material_id: number;
  chapter: string;
  chunk_idx: number;
}

export interface ChatResult {
  answer: string;
  citations: Citation[];
}

const TOKEN_KEY = "lb_token";

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

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
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
  return text ? (JSON.parse(text) as T) : ({} as T);
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
    request<{ materials: Material[] }>("GET", `/teams/${id}/materials`),

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
  getMaterial: (id: number) => request<{ material: Material }>("GET", `/materials/${id}`),
  updateMaterial: (id: number, body: { title?: string; content?: string; shared?: boolean }) =>
    request<{ material: Material }>("PUT", `/materials/${id}`, body),
  deleteMaterial: (id: number) => request<{ ok: boolean }>("DELETE", `/materials/${id}`),

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
  listSessions: () => request<{ sessions: AgentSession[] }>("GET", "/agent/sessions"),

  // SSE 答疑（fetch + ReadableStream，避免 token 进 URL，呼应 R4）
  chatStream: async (
    req: { question: string; session_id?: string; material_id?: number },
    handlers: {
      onToken: (t: string) => void;
      onCitations: (c: Citation[]) => void;
      onEnd: () => void;
      onError: (m: string) => void;
    },
  ): Promise<void> => {
    try {
      await streamPost("/agent/chat", req, (payload) => {
        if (!payload) return;
        try {
          const ev = JSON.parse(payload) as {
            type: string;
            text?: string;
            citations?: Citation[];
          };
          if (ev.type === "token" && ev.text) handlers.onToken(ev.text);
          else if (ev.type === "done" && ev.citations) handlers.onCitations(ev.citations);
          else if (ev.type === "error" && ev.text) handlers.onError(ev.text);
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
