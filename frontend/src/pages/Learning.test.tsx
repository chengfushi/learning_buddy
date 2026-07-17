// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type LearningRecord, type ProgressSummary } from "../api";
import Learning from "./Learning";

const summary: ProgressSummary = {
  total_duration_s: 125,
  avg_progress: 68.4,
  quiz_count: 2,
  quiz_correct: 1,
  quiz_accuracy: 50.6,
  daily: [],
};

const scoredRecord: LearningRecord = {
  ID: 51,
  UserID: 7,
  MaterialID: null,
  DurationS: 600,
  Progress: 75.4,
  Score: 88.6,
  CreatedAt: "2026-07-15T10:00:00Z",
};

const unscoredRecord: LearningRecord = {
  ...scoredRecord,
  ID: 52,
  DurationS: 90,
  Progress: 10,
  Score: null,
  CreatedAt: "2026-07-14T10:00:00Z",
};

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

describe("Learning", () => {
  beforeEach(() => {
    vi.spyOn(api, "getProgress").mockResolvedValue({ summary });
    vi.spyOn(api, "listLearningRecords").mockResolvedValue({
      records: [scoredRecord, unscoredRecord],
    });
    vi.spyOn(api, "createLearningRecord").mockResolvedValue({ record: scoredRecord });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("keeps summary and record loading states separate from loaded values", async () => {
    const summaryResult = deferred<{ summary: ProgressSummary }>();
    const recordsResult = deferred<{ records: LearningRecord[] }>();
    vi.mocked(api.getProgress).mockReturnValue(summaryResult.promise);
    vi.mocked(api.listLearningRecords).mockReturnValue(recordsResult.promise);
    const { container } = render(<Learning />);

    expect(screen.getByText("正在加载学习统计…")).not.toBeNull();
    expect(screen.getByText("正在加载学习记录…")).not.toBeNull();
    expect(screen.queryByText("还没有学习记录，去资料里学一会吧。")).toBeNull();

    await act(async () => summaryResult.resolve({ summary }));
    await waitFor(() => expect(container.querySelector(".stats")).not.toBeNull());
    expect([...container.querySelectorAll(".stats .num")].map((node) => node.textContent)).toEqual([
      "2",
      "68%",
      "2",
      "51%",
    ]);
    expect(screen.getByText("正在加载学习记录…")).not.toBeNull();

    await act(async () => recordsResult.resolve({ records: [scoredRecord, unscoredRecord] }));
    await waitFor(() => expect(screen.getByText(/2026-07-15 · 10 分钟/)).not.toBeNull());
    expect(screen.getByText(/完成度 75% · 得分 89/)).not.toBeNull();
    expect(screen.getByText(/2026-07-14 · 2 分钟 · 完成度 10%/)).not.toBeNull();
  });

  it("renders an explicit empty state after an empty record response", async () => {
    vi.mocked(api.listLearningRecords).mockResolvedValue({ records: [] });
    render(<Learning />);

    await waitFor(() =>
      expect(screen.getByText("还没有学习记录，去资料里学一会吧。")).not.toBeNull(),
    );
    expect(screen.queryByText("正在加载学习记录…")).toBeNull();
  });

  it("converts UI minutes to API seconds, keeps percent units, and reloads records", async () => {
    const newRecord = { ...scoredRecord, DurationS: 900, Progress: 75, Score: 88.5 };
    vi.mocked(api.listLearningRecords)
      .mockResolvedValueOnce({ records: [] })
      .mockResolvedValue({ records: [newRecord] });
    const { container } = render(<Learning />);
    await screen.findByText("还没有学习记录，去资料里学一会吧。");

    const duration = screen.getByLabelText("时长（分钟）") as HTMLInputElement;
    const progress = screen.getByLabelText("完成度（0-100）") as HTMLInputElement;
    const score = screen.getByLabelText("得分（可选）") as HTMLInputElement;
    fireEvent.change(duration, { target: { value: "15" } });
    fireEvent.change(progress, { target: { value: "75" } });
    fireEvent.change(score, { target: { value: "88.5" } });
    fireEvent.submit(requireForm(container));

    await waitFor(() =>
      expect(api.createLearningRecord).toHaveBeenNthCalledWith(1, {
        duration_s: 900,
        progress: 75,
        score: 88.5,
      }),
    );
    await screen.findByText(/15 分钟 · 完成度 75% · 得分 89/);
    expect(progress.value).toBe("0");
    expect(score.value).toBe("");

    fireEvent.submit(requireForm(container));
    await waitFor(() =>
      expect(api.createLearningRecord).toHaveBeenNthCalledWith(2, {
        duration_s: 900,
        progress: 0,
        score: undefined,
      }),
    );
  });

  it.each([
    [new Error("统计加载失败"), "统计加载失败"],
    ["network", "加载失败"],
  ])("reports progress summary failures", async (reason, message) => {
    vi.mocked(api.getProgress).mockRejectedValue(reason);
    render(<Learning />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载学习统计…")).toBeNull();
  });

  it.each([
    [new Error("记录服务不可用"), "记录服务不可用"],
    ["network", "学习记录加载失败"],
  ])("reports record list failures", async (reason, message) => {
    vi.mocked(api.listLearningRecords).mockRejectedValue(reason);
    render(<Learning />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载学习记录…")).toBeNull();
    expect(screen.getByText("还没有学习记录，去资料里学一会吧。")).not.toBeNull();
  });

  it("reports Error and non-Error record creation failures", async () => {
    vi.mocked(api.createLearningRecord)
      .mockRejectedValueOnce(new Error("记录内容无效"))
      .mockRejectedValueOnce("network");
    const { container } = render(<Learning />);
    await screen.findByText(/2026-07-15 · 10 分钟/);
    const form = requireForm(container);

    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByText("记录内容无效")).not.toBeNull());

    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByText("记录失败")).not.toBeNull());
    expect(api.createLearningRecord).toHaveBeenCalledTimes(2);
  });
});

function requireForm(container: HTMLElement): HTMLFormElement {
  const form = container.querySelector("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("learning record form not found");
  return form;
}
