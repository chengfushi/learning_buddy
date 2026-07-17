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

  it("keeps the SSE access token in the Authorization header and out of the URL", async () => {
    const token = "sensitive-sse-token";
    vi.mocked(localStorage.getItem).mockReturnValue(token);
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
});
