import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

const materialResponse = {
  ID: 42,
  TeamID: 7,
  Title: "函数基础",
  Subject: null,
  Chapter: null,
  Tags: null,
  Content: "测试内容",
  FileType: null,
  ParseStatus: "pending",
  ParseGeneration: 2,
  ParseError: null,
  Shared: false,
  OwnerID: 9,
  CreatedAt: "2026-07-17T00:00:00Z",
};

describe("api", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("posts to the failed material retry endpoint", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ material: materialResponse }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await api.retryMaterialParse(42);

    expect(result.material).toMatchObject({ ID: 42, ParseStatus: "pending" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/materials/42/retry",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("refreshes once after a 401 and retries with the new in-memory token", async () => {
    const { setToken, getToken } = await import("./api");
    setToken("expired-token");
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "expired" }), { status: 401 }))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ access_token: "rotated-token" }), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ user: { ID: 1 } }), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.me()).resolves.toEqual({ user: { ID: 1 } });
    expect(getToken()).toBe("rotated-token");
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/auth/refresh", expect.anything());
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/me", expect.anything());
  });

  it("rejects an invalid team-material write capability contract", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        new Response(JSON.stringify({ materials: [materialResponse], can_write: "yes" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(api.listTeamMaterials(7)).rejects.toThrow();
  });

  it("validates scoped session summaries and structured message history", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            sessions: [
              {
                ID: "session-31",
                UserID: 9,
                MaterialID: 31,
                Title: "资料会话",
                CreatedAt: "2026-07-18T08:00:00Z",
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            session_id: "session-31",
            messages: [
              {
                ID: 81,
                Role: "assistant",
                Content: "有据回答",
                Citations: [
                  {
                    team_id: 7,
                    material_id: 31,
                    chapter: "第一章",
                    chunk_idx: 0,
                    chunk_id: 301,
                    title: "函数基础",
                    page_number: 2,
                  },
                ],
                CreatedAt: "2026-07-18T08:00:01Z",
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    const listed = await api.listSessions();
    const history = await api.getSession("session-31");

    expect(listed.sessions[0]).toMatchObject({ ID: "session-31", MaterialID: 31 });
    expect(history.messages[0].Citations[0]).toMatchObject({
      material_id: 31,
      chunk_id: 301,
      page_number: 2,
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/agent/sessions/session-31",
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("rejects malformed session history instead of rendering untrusted citation shapes", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(
        new Response(
          JSON.stringify({
            session_id: "session-31",
            messages: [
              {
                ID: 81,
                Role: "assistant",
                Content: "回答",
                Citations: "not-an-array",
                CreatedAt: "2026-07-18T08:00:01Z",
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    await expect(api.getSession("session-31")).rejects.toThrow();
  });

  it("keeps the SSE access token in the Authorization header and out of the URL", async () => {
    const token = "sensitive-sse-token";
    vi.mocked(localStorage.getItem).mockReturnValue(null);
    const { setToken } = await import("./api");
    setToken(token);
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response('data: {"type":"done","citations":[]}\n\n', {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const onEnd = vi.fn();

    await api.chatStream(
      { question: "测试 SSE 鉴权" },
      {
        onToken: vi.fn(),
        onCitations: vi.fn(),
        onEnd,
        onError: vi.fn(),
      },
    );

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/agent/chat");
    expect(String(url)).not.toContain(token);
    expect(init?.headers).toMatchObject({ Authorization: `Bearer ${token}` });
    expect(onEnd).toHaveBeenCalledOnce();
  });

  it("validates chunked SSE events and flushes a final line without a newline", async () => {
    const encoder = new TextEncoder();
    const payload = [
      'data: {"type":"token","text":"回答"}\n\n',
      'data: {"type":"done","session_id":"bad","stage_ms":{"retrieve":"slow"}}\n\n',
      'data: {"type":"done","session_id":"session-zero","message_id":0,"citations":[]}\n\n',
      'data: {"type":"error","message":"模型超时"}\n\n',
      'data: {"type":"done","session_id":"session-1","message_id":9,',
      '"citations":[],"stage_ms":{"retrieve":80},"degraded_stages":["rerank"]}',
    ].join("");
    const bytes = encoder.encode(payload);
    const splitAt = payload.indexOf("答") + 1;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(bytes.slice(0, splitAt));
        controller.enqueue(bytes.slice(splitAt, bytes.length - 11));
        controller.enqueue(bytes.slice(bytes.length - 11));
        controller.close();
      },
    });
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } }),
        ),
    );
    const onToken = vi.fn();
    const onDone = vi.fn();
    const onError = vi.fn();

    await api.chatStream({ question: "测试分块" }, { onToken, onDone, onEnd: vi.fn(), onError });

    expect(onToken).toHaveBeenCalledWith("回答");
    expect(onDone).toHaveBeenCalledOnce();
    expect(onDone).toHaveBeenCalledWith({
      session_id: "session-1",
      message_id: 9,
      citations: [],
      stage_ms: { retrieve: 80 },
      degraded_stages: ["rerank"],
    });
    expect(onError).toHaveBeenCalledOnce();
    expect(onError).toHaveBeenCalledWith("模型超时");
  });
});
