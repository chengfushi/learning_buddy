// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type Material, type MaterialNote } from "../api";
import Reader from "./Reader";

const material: Material = {
  ID: 31,
  TeamID: 1,
  Title: "牛顿运动定律",
  Subject: "物理",
  Chapter: null,
  Tags: null,
  Content: "F=ma",
  FileType: "txt",
  ParseStatus: "done",
  ParseGeneration: 1,
  ParseError: null,
  Shared: true,
  OwnerID: 7,
  CreatedAt: "2026-07-15T00:00:00Z",
};

const note: MaterialNote = {
  ID: 41,
  UserID: 7,
  MaterialID: material.ID,
  Content: "注意统一单位",
  Quote: null,
  CreatedAt: "2026-07-15T00:00:00Z",
  UpdatedAt: "2026-07-15T00:00:00Z",
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

describe("Reader", () => {
  beforeEach(() => {
    vi.spyOn(api, "getMaterial").mockResolvedValue({ material });
    vi.spyOn(api, "listNotes").mockResolvedValue({ notes: [note] });
    vi.spyOn(api, "createNote").mockResolvedValue({ note });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("keeps material and note loading states separate from their content and empty states", async () => {
    const materialResult = deferred<{ material: Material }>();
    const notesResult = deferred<{ notes: MaterialNote[] }>();
    vi.mocked(api.getMaterial).mockReturnValue(materialResult.promise);
    vi.mocked(api.listNotes).mockReturnValue(notesResult.promise);
    render(<Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />);

    expect(screen.getByText("正在加载资料…")).not.toBeNull();
    expect(screen.getByText("正在加载笔记…")).not.toBeNull();
    expect(screen.queryByText("还没有笔记。")).toBeNull();

    await act(async () => materialResult.resolve({ material }));
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: material.Title })).not.toBeNull(),
    );
    expect(screen.getByText("物理 · 状态：done · 已共享")).not.toBeNull();
    expect(screen.getByText(material.Content as string)).not.toBeNull();
    expect(screen.getByText("正在加载笔记…")).not.toBeNull();

    await act(async () => notesResult.resolve({ notes: [note] }));
    await waitFor(() => expect(screen.getByText(note.Content)).not.toBeNull());
    expect(screen.queryByText("正在加载笔记…")).toBeNull();
  });

  it("shows explicit fallbacks for missing chapter, body, and notes", async () => {
    vi.mocked(api.getMaterial).mockResolvedValue({
      material: {
        ...material,
        Subject: null,
        Chapter: null,
        Content: null,
        Shared: false,
      },
    });
    vi.mocked(api.listNotes).mockResolvedValue({ notes: [] });
    render(<Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("无章节 · 状态：done")).not.toBeNull());
    expect(screen.getByText("（暂无正文，可能是未解析的文件类资料）")).not.toBeNull();
    expect(screen.getByText("还没有笔记。")).not.toBeNull();
  });

  it("calls back for navigation and material-scoped AI questions", () => {
    const onBack = vi.fn();
    const onAsk = vi.fn();
    render(<Reader materialId={material.ID} onBack={onBack} onAsk={onAsk} />);

    fireEvent.click(screen.getByRole("button", { name: "‹ 返回" }));
    fireEvent.click(screen.getByRole("button", { name: "用 AI 学伴提问此资料" }));

    expect(onBack).toHaveBeenCalledOnce();
    expect(onAsk).toHaveBeenCalledWith(material.ID);
  });

  it("adds a note, clears the editor, and reloads material plus notes", async () => {
    vi.mocked(api.listNotes)
      .mockResolvedValueOnce({ notes: [] })
      .mockResolvedValueOnce({ notes: [note] });
    const { container } = render(
      <Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />,
    );
    await screen.findByText("还没有笔记。");
    const editor = screen.getByPlaceholderText("写下你的理解或标注…") as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: note.Content } });
    fireEvent.submit(requireForm(container));

    await waitFor(() => expect(api.createNote).toHaveBeenCalledWith(material.ID, note.Content));
    await screen.findByText(note.Content);
    expect(editor.value).toBe("");
    expect(api.getMaterial).toHaveBeenCalledTimes(2);
    expect(api.listNotes).toHaveBeenCalledTimes(2);
  });

  it("reloads isolated content when materialId changes", async () => {
    const second = { ...material, ID: 32, Title: "动量守恒" };
    vi.mocked(api.getMaterial).mockImplementation(async (id) => ({
      material: id === material.ID ? material : second,
    }));
    const { rerender } = render(
      <Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />,
    );
    await screen.findByRole("heading", { name: material.Title });

    rerender(<Reader materialId={second.ID} onBack={vi.fn()} onAsk={vi.fn()} />);

    await screen.findByRole("heading", { name: second.Title });
    expect(screen.queryByRole("heading", { name: material.Title })).toBeNull();
    expect(api.getMaterial).toHaveBeenLastCalledWith(second.ID);
    expect(api.listNotes).toHaveBeenLastCalledWith(second.ID);
  });

  it.each([
    [new Error("资料加载失败"), "资料加载失败"],
    ["network", "加载失败"],
  ])("reports material loading failures", async (reason, message) => {
    vi.mocked(api.getMaterial).mockRejectedValue(reason);
    render(<Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载资料…")).toBeNull();
  });

  it.each([
    [new Error("笔记服务不可用"), "笔记服务不可用"],
    ["network", "笔记加载失败"],
  ])("reports note loading failures", async (reason, message) => {
    vi.mocked(api.listNotes).mockRejectedValue(reason);
    render(<Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载笔记…")).toBeNull();
    expect(screen.getByText("还没有笔记。")).not.toBeNull();
  });

  it("reports Error and non-Error note creation failures", async () => {
    vi.mocked(api.createNote)
      .mockRejectedValueOnce(new Error("笔记内容不合法"))
      .mockRejectedValueOnce("network");
    const { container } = render(
      <Reader materialId={material.ID} onBack={vi.fn()} onAsk={vi.fn()} />,
    );
    await screen.findByText(note.Content);
    const form = requireForm(container);

    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByText("笔记内容不合法")).not.toBeNull());

    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByText("笔记保存失败")).not.toBeNull());
    expect(api.createNote).toHaveBeenCalledTimes(2);
  });
});

function requireForm(container: HTMLElement): HTMLFormElement {
  const form = container.querySelector("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("note form not found");
  return form;
}
