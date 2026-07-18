// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type Citation, type Exercise, type StudyPlan } from "../api";
import Companion from "./Companion";

const plan: StudyPlan = {
  ID: 61,
  UserID: 7,
  Title: "两周物理计划",
  Goal: "掌握牛顿定律",
  Deadline: "2026-07-30T00:00:00Z",
  Items: [
    { date: "D1", task: "复习概念", done: false },
    { date: "D2", task: "完成练习", done: false },
  ],
  CreatedAt: "2026-07-15T00:00:00Z",
};

const exercises: Exercise[] = [
  {
    ID: 71,
    MaterialID: 31,
    Question: "牛顿第一定律描述什么？",
    Options: ["惯性", "加速度"],
    Difficulty: "medium",
    CreatedAt: "2026-07-15T00:00:00Z",
  },
  {
    ID: 72,
    MaterialID: 31,
    Question: "F=ma 中 m 表示什么？",
    Options: ["质量", "速度"],
    Difficulty: "medium",
    CreatedAt: "2026-07-15T00:00:00Z",
  },
  {
    ID: 73,
    MaterialID: null,
    Question: "暂无选项题",
    Options: null,
    Difficulty: null,
    CreatedAt: "2026-07-15T00:00:00Z",
  },
];

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve: ((value: T) => void) | undefined;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return {
    promise,
    resolve(value: T) {
      if (!resolve) throw new Error("deferred resolver unavailable");
      resolve(value);
    },
  };
}

function createMemoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, value),
  };
}

describe("Companion", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", createMemoryStorage());
    vi.spyOn(api, "chatStream").mockResolvedValue(undefined);
    vi.spyOn(api, "createPlan").mockResolvedValue({ plan });
    vi.spyOn(api, "createQuiz").mockResolvedValue({ exercises });
    vi.spyOn(api, "answerQuiz").mockResolvedValue({
      is_correct: true,
      correct_key: "A",
      attempt: {},
    });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("shows material-scoped and general chat placeholders and ignores blank questions", () => {
    const { rerender } = render(<Companion materialId={31} />);
    expect(screen.getByPlaceholderText("针对当前资料提问…")).not.toBeNull();
    expect(screen.getByRole("button", { name: "AI 答疑" }).className).toBe("on");

    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    expect(api.chatStream).not.toHaveBeenCalled();

    rerender(<Companion />);
    expect(screen.getByPlaceholderText("向学伴提问…")).not.toBeNull();
  });

  it("isolates persisted sessions between global and material-scoped chats", async () => {
    globalThis.localStorage.setItem("lb_chat_session:global", "global-session");
    globalThis.localStorage.setItem("lb_chat_session:material:31", "material-31-session");
    const { rerender } = render(<Companion materialId={31} />);

    fireEvent.change(screen.getByPlaceholderText("针对当前资料提问…"), {
      target: { value: "资料问题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(api.chatStream).toHaveBeenLastCalledWith(
        {
          question: "资料问题",
          material_id: 31,
          session_id: "material-31-session",
        },
        expect.any(Object),
      ),
    );

    rerender(<Companion />);
    fireEvent.change(screen.getByPlaceholderText("向学伴提问…"), {
      target: { value: "全局问题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(api.chatStream).toHaveBeenLastCalledWith(
        { question: "全局问题", material_id: undefined, session_id: "global-session" },
        expect.any(Object),
      ),
    );
  });

  it("streams tokens and citations, prevents duplicate sends, and restores the composer", async () => {
    const stream = deferred<void>();
    const onOpenMaterial = vi.fn();
    let handlers: Parameters<typeof api.chatStream>[1] | undefined;
    vi.mocked(api.chatStream).mockImplementation((_, nextHandlers) => {
      handlers = nextHandlers;
      return stream.promise;
    });
    render(<Companion materialId={31} onOpenMaterial={onOpenMaterial} />);
    const input = screen.getByPlaceholderText("针对当前资料提问…") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "  什么是惯性？  " } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(api.chatStream).toHaveBeenCalledOnce());
    expect(api.chatStream).toHaveBeenCalledWith(
      { question: "什么是惯性？", material_id: 31 },
      expect.any(Object),
    );
    expect(input.value).toBe("");
    expect(input.disabled).toBe(true);
    expect((screen.getByRole("button", { name: "思考中…" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(screen.getByText("什么是惯性？")).not.toBeNull();
    expect(screen.getByText("…")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "思考中…" }));
    expect(api.chatStream).toHaveBeenCalledOnce();

    await act(async () => {
      if (!handlers) throw new Error("chat handlers unavailable");
      handlers.onToken("牛顿");
      handlers.onToken("第一定律");
      const citations: Citation[] = [
        {
          team_id: 1,
          material_id: 31,
          chapter: "第一章",
          chunk_idx: 0,
          page_number: 3,
          asset_id: 81,
        },
        { team_id: 1, material_id: 32, chapter: "", chunk_idx: 1 },
      ];
      handlers.onCitations?.(citations);
      handlers.onDone?.({
        session_id: "session-31",
        message_id: 91,
        citations,
        stage_ms: { analyze_query: 120, retrieve: 80, rerank: 240, expand: Number.NaN },
        degraded_stages: ["embedding", "rerank", "rerank"],
      });
      handlers.onEnd();
      stream.resolve();
    });

    await waitFor(() => expect(screen.getByText("牛顿第一定律")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "资料#31·第 3 页" }));
    expect(onOpenMaterial).toHaveBeenCalledWith({ materialId: 31, pageNumber: 3, assetId: 81 });
    expect(screen.getByText("资料#32")).not.toBeNull();
    expect(screen.getByRole("status").textContent).toBe(
      "本次检索使用了降级路径：语义检索、候选精排",
    );
    expect(screen.getByText("问题理解").closest("div")?.textContent).toBe("问题理解120 ms");
    expect(screen.getByText("知识库召回").closest("div")?.textContent).toBe("知识库召回80 ms");
    expect(screen.getByText("候选精排").closest("div")?.textContent).toBe("候选精排240 ms");
    expect(screen.queryByText("上下文扩展")).toBeNull();
    expect(screen.getByRole("button", { name: "发送" })).not.toBeNull();
    expect(input.disabled).toBe(false);
  });

  it("renders SSE callback errors and releases the busy state", async () => {
    vi.mocked(api.chatStream).mockImplementation(async (_, handlers) => {
      handlers.onError("检索超时");
    });
    render(<Companion />);
    const input = screen.getByPlaceholderText("向学伴提问…");
    fireEvent.change(input, { target: { value: "测试错误" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() => expect(screen.getByText("出错了：检索超时")).not.toBeNull());
    expect(screen.getByRole("button", { name: "发送" })).not.toBeNull();
  });

  it.each([
    [new Error("连接中断"), "出错了：连接中断"],
    ["network", "出错了：对话请求失败"],
  ])("handles unexpected chat promise rejections", async (reason, message) => {
    vi.mocked(api.chatStream).mockRejectedValue(reason);
    render(<Companion />);
    fireEvent.change(screen.getByPlaceholderText("向学伴提问…"), {
      target: { value: "测试异常" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.getByRole("button", { name: "发送" })).not.toBeNull();
  });

  it("generates a dated plan with busy protection and renders its items", async () => {
    const result = deferred<{ plan: StudyPlan }>();
    vi.mocked(api.createPlan).mockReturnValue(result.promise);
    const { container } = render(<Companion />);
    fireEvent.click(screen.getByRole("button", { name: "学习计划" }));
    expect(screen.getByRole("button", { name: "学习计划" }).className).toBe("on");

    fireEvent.click(screen.getByRole("button", { name: "生成学习计划" }));
    expect(api.createPlan).not.toHaveBeenCalled();
    fireEvent.change(screen.getByPlaceholderText("学习目标，例如：掌握人工智能基础"), {
      target: { value: plan.Goal },
    });
    const deadline = container.querySelector<HTMLInputElement>('input[type="date"]');
    if (!deadline) throw new Error("deadline input not found");
    fireEvent.change(deadline, { target: { value: "2026-07-30" } });
    fireEvent.click(screen.getByRole("button", { name: "生成学习计划" }));

    await waitFor(() => expect(api.createPlan).toHaveBeenCalledWith(plan.Goal, "2026-07-30"));
    expect((screen.getByRole("button", { name: "生成中…" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "生成中…" }));
    expect(api.createPlan).toHaveBeenCalledOnce();

    await act(async () => result.resolve({ plan }));
    await screen.findByRole("heading", { name: plan.Title });
    expect(screen.getByText("D1").closest("li")?.textContent).toContain("复习概念");
    expect(screen.getByText("D2").closest("li")?.textContent).toContain("完成练习");
  });

  it("passes undefined for an empty deadline and supports plans without items", async () => {
    vi.mocked(api.createPlan).mockResolvedValue({ plan: { ...plan, Items: null } });
    render(<Companion />);
    fireEvent.click(screen.getByRole("button", { name: "学习计划" }));
    fireEvent.change(screen.getByPlaceholderText("学习目标，例如：掌握人工智能基础"), {
      target: { value: plan.Goal },
    });
    fireEvent.click(screen.getByRole("button", { name: "生成学习计划" }));

    await waitFor(() => expect(api.createPlan).toHaveBeenCalledWith(plan.Goal, undefined));
    expect(await screen.findByRole("heading", { name: plan.Title })).not.toBeNull();
  });

  it.each([
    [new Error("目标不可用"), "目标不可用"],
    ["network", "计划生成失败"],
  ])("reports plan generation failures", async (reason, message) => {
    vi.mocked(api.createPlan).mockRejectedValue(reason);
    render(<Companion />);
    fireEvent.click(screen.getByRole("button", { name: "学习计划" }));
    fireEvent.change(screen.getByPlaceholderText("学习目标，例如：掌握人工智能基础"), {
      target: { value: plan.Goal },
    });
    fireEvent.click(screen.getByRole("button", { name: "生成学习计划" }));

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.getByRole("button", { name: "生成学习计划" })).not.toBeNull();
  });

  it("generates quizzes with busy protection, grades correct and incorrect answers, and resets results", async () => {
    const quizResult = deferred<{ exercises: Exercise[] }>();
    vi.mocked(api.createQuiz)
      .mockReturnValueOnce(quizResult.promise)
      .mockResolvedValue({ exercises });
    vi.mocked(api.answerQuiz)
      .mockResolvedValueOnce({ is_correct: true, correct_key: "A", attempt: {} })
      .mockResolvedValueOnce({ is_correct: false, correct_key: "A", attempt: {} });
    const { container } = render(<Companion materialId={31} />);
    fireEvent.click(screen.getByRole("button", { name: "智能测评" }));
    expect(screen.getByRole("button", { name: "智能测评" }).className).toBe("on");

    fireEvent.click(screen.getByRole("button", { name: "生成测评" }));
    expect(api.createQuiz).not.toHaveBeenCalled();
    fireEvent.change(screen.getByPlaceholderText("测评主题，例如：机器学习"), {
      target: { value: "牛顿定律" },
    });
    const count = container.querySelector<HTMLInputElement>('input[type="number"]');
    if (!count) throw new Error("quiz count input not found");
    fireEvent.change(count, { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: "生成测评" }));
    await waitFor(() => expect(api.createQuiz).toHaveBeenCalledWith("牛顿定律", 3, 31));
    expect((screen.getByRole("button", { name: "生成中…" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "生成中…" }));
    expect(api.createQuiz).toHaveBeenCalledOnce();

    await act(async () => quizResult.resolve({ exercises }));
    await screen.findByText(exercises[0].Question);
    expect(screen.getByText(exercises[2].Question)).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "惯性" }));
    await screen.findByText("✓ 回答正确");
    expect(api.answerQuiz).toHaveBeenNthCalledWith(1, 71, "A");
    expect((screen.getByRole("button", { name: "惯性" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "速度" }));
    await screen.findByText("✗ 回答错误");
    expect(api.answerQuiz).toHaveBeenNthCalledWith(2, 72, "B");

    fireEvent.click(screen.getByRole("button", { name: "生成测评" }));
    await waitFor(() => expect(screen.queryByText("✓ 回答正确")).toBeNull());
    expect((screen.getByRole("button", { name: "惯性" }) as HTMLButtonElement).disabled).toBe(
      false,
    );
  });

  it.each([
    [new Error("主题无效"), "主题无效"],
    ["network", "测评生成失败"],
  ])("reports quiz generation failures", async (reason, message) => {
    vi.mocked(api.createQuiz).mockRejectedValue(reason);
    render(<Companion />);
    fireEvent.click(screen.getByRole("button", { name: "智能测评" }));
    fireEvent.change(screen.getByPlaceholderText("测评主题，例如：机器学习"), {
      target: { value: "测试主题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "生成测评" }));

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.getByRole("button", { name: "生成测评" })).not.toBeNull();
  });

  it.each([
    [new Error("答案无效"), "答案无效"],
    ["network", "答案提交失败"],
  ])("reports answer submission failures and re-enables options", async (reason, message) => {
    vi.mocked(api.answerQuiz).mockRejectedValue(reason);
    render(<Companion />);
    fireEvent.click(screen.getByRole("button", { name: "智能测评" }));
    fireEvent.change(screen.getByPlaceholderText("测评主题，例如：机器学习"), {
      target: { value: "测试主题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "生成测评" }));
    await screen.findByText(exercises[0].Question);

    const option = screen.getByRole("button", { name: "惯性" }) as HTMLButtonElement;
    fireEvent.click(option);
    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(option.disabled).toBe(false);
  });
});
