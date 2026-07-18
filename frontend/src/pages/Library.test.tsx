// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type Material, type MaterialProcessing, type Team } from "../api";
import Library from "./Library";

const writableTeam: Team = {
  ID: 1,
  Name: "物理知识库",
  Type: "private",
  JoinCode: null,
  OwnerID: 7,
  CreatedAt: "2026-07-15T00:00:00Z",
};

const readOnlyTeam: Team = {
  ...writableTeam,
  ID: 2,
  Name: "老师共享库",
  Type: "teacher",
  JoinCode: "ABC123",
  OwnerID: 8,
};

const failedMaterial: Material = {
  ID: 11,
  TeamID: writableTeam.ID,
  Title: "待重试资料",
  Subject: "物理",
  Chapter: null,
  Tags: null,
  Content: "正文",
  FileType: "txt",
  ParseStatus: "failed",
  ParseGeneration: 1,
  ParseError: "worker timeout",
  Shared: false,
  OwnerID: 7,
  CreatedAt: "2026-07-15T00:00:00Z",
};

const readyMaterial: Material = {
  ...failedMaterial,
  ID: 12,
  Title: "牛顿定律",
  Subject: null,
  Chapter: "第一章",
  ParseStatus: "done",
  ParseGeneration: 1,
  ParseError: null,
  Shared: true,
};

const parsingMaterial: Material = {
  ...readyMaterial,
  ID: 13,
  Title: "正在解析的资料",
  ParseStatus: "parsing",
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

describe("Library", () => {
  beforeEach(() => {
    vi.spyOn(api, "listTeams").mockResolvedValue({ teams: [writableTeam] });
    vi.spyOn(api, "listTeamMaterials").mockResolvedValue({ materials: [], can_write: true });
    vi.spyOn(api, "createMaterial").mockResolvedValue({ material: readyMaterial });
    vi.spyOn(api, "retryMaterialParse").mockResolvedValue({ material: readyMaterial });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("keeps loading and empty states distinct and hides writes for a read-only team", async () => {
    const teams = deferred<{ teams: Team[] }>();
    const materials = deferred<{ materials: Material[]; can_write: boolean }>();
    vi.mocked(api.listTeams).mockReturnValue(teams.promise);
    vi.mocked(api.listTeamMaterials).mockReturnValue(materials.promise);

    render(<Library onOpenMaterial={vi.fn()} />);

    expect(screen.getByText("正在加载知识库…")).not.toBeNull();
    expect(screen.getByText("正在加载资料…")).not.toBeNull();
    expect(screen.queryByText("该知识库暂无资料。")).toBeNull();

    await act(async () => teams.resolve({ teams: [readOnlyTeam] }));
    await waitFor(() => expect(api.listTeamMaterials).toHaveBeenCalledWith(readOnlyTeam.ID));
    expect(screen.getByText("正在加载资料…")).not.toBeNull();

    await act(async () => materials.resolve({ materials: [], can_write: false }));
    await waitFor(() => expect(screen.getByText("该知识库暂无资料。")).not.toBeNull());
    expect(screen.queryByRole("button", { name: "+ 新建资料" })).toBeNull();
  });

  it("opens materials and retries a failed parse without triggering row navigation", async () => {
    const retry = deferred<{ material: Material }>();
    vi.mocked(api.listTeamMaterials).mockResolvedValue({
      materials: [failedMaterial, readyMaterial],
      can_write: true,
    });
    vi.mocked(api.retryMaterialParse).mockReturnValue(retry.promise);
    const onOpenMaterial = vi.fn();
    render(<Library onOpenMaterial={onOpenMaterial} />);
    await screen.findByText(failedMaterial.Title);

    fireEvent.click(screen.getByText(readyMaterial.Title).closest("li") as HTMLLIElement);
    expect(onOpenMaterial).toHaveBeenCalledWith(readyMaterial.ID);
    onOpenMaterial.mockClear();

    fireEvent.click(screen.getByRole("button", { name: "重试解析" }));
    expect((screen.getByRole("button", { name: "重试中…" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(onOpenMaterial).not.toHaveBeenCalled();

    await act(async () =>
      retry.resolve({ material: { ...failedMaterial, ParseStatus: "pending", ParseError: null } }),
    );
    await waitFor(() => expect(screen.getByText(/状态：pending/)).not.toBeNull());
    expect(screen.queryByRole("button", { name: "重试解析" })).toBeNull();
    expect(screen.getByText(/第一章 · 状态：done · 已共享/)).not.toBeNull();
  });

  it("switches teams without exposing stale write controls or materials", async () => {
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [writableTeam, readOnlyTeam] });
    vi.mocked(api.listTeamMaterials).mockImplementation(async (teamID) =>
      teamID === writableTeam.ID
        ? { materials: [readyMaterial], can_write: true }
        : { materials: [], can_write: false },
    );
    const { container } = render(<Library onOpenMaterial={vi.fn()} />);
    await screen.findByText(readyMaterial.Title);
    expect(screen.getByRole("button", { name: "+ 新建资料" })).not.toBeNull();

    const teamItems = container.querySelectorAll<HTMLElement>(".side-item");
    fireEvent.click(teamItems[1]);
    expect(screen.queryByText(readyMaterial.Title)).toBeNull();
    expect(screen.queryByRole("button", { name: "+ 新建资料" })).toBeNull();

    await waitFor(() => expect(screen.getByText("该知识库暂无资料。")).not.toBeNull());
    expect(screen.queryByText(readyMaterial.Title)).toBeNull();
    expect(screen.queryByRole("button", { name: "+ 新建资料" })).toBeNull();
    expect(teamItems[1].className).toBe("side-item on");
    expect(teamItems[0].className).toBe("side-item");
  });

  it("ignores a writable material response that arrives after switching to a read-only team", async () => {
    const writableMaterials = deferred<{ materials: Material[]; can_write: boolean }>();
    const readOnlyMaterials = deferred<{ materials: Material[]; can_write: boolean }>();
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [writableTeam, readOnlyTeam] });
    vi.mocked(api.listTeamMaterials).mockImplementation((teamID) =>
      teamID === writableTeam.ID ? writableMaterials.promise : readOnlyMaterials.promise,
    );
    render(<Library onOpenMaterial={vi.fn()} />);
    await waitFor(() => expect(api.listTeamMaterials).toHaveBeenCalledWith(writableTeam.ID));

    fireEvent.click(screen.getByText(readOnlyTeam.Name));
    await waitFor(() => expect(api.listTeamMaterials).toHaveBeenCalledWith(readOnlyTeam.ID));
    await act(async () => readOnlyMaterials.resolve({ materials: [], can_write: false }));
    await screen.findByText("该知识库暂无资料。");
    await act(async () =>
      writableMaterials.resolve({ materials: [readyMaterial], can_write: true }),
    );

    expect(screen.queryByText(readyMaterial.Title)).toBeNull();
    expect(screen.queryByRole("button", { name: "+ 新建资料" })).toBeNull();
    expect(
      screen.getByText(readOnlyTeam.Name, { selector: ".side-item span" }).closest(".side-item")
        ?.className,
    ).toBe("side-item on");
  });

  it("ignores terminal processing results from a team that is no longer selected", async () => {
    const processing = deferred<{ processing: MaterialProcessing | null }>();
    vi.spyOn(api, "getMaterialProcessing").mockReturnValue(processing.promise);
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [writableTeam, readOnlyTeam] });
    vi.mocked(api.listTeamMaterials).mockImplementation(async (teamID) =>
      teamID === writableTeam.ID
        ? { materials: [parsingMaterial], can_write: true }
        : { materials: [], can_write: false },
    );
    render(<Library onOpenMaterial={vi.fn()} />);
    await screen.findByText(parsingMaterial.Title);
    await waitFor(() => expect(api.getMaterialProcessing).toHaveBeenCalledWith(parsingMaterial.ID));

    fireEvent.click(screen.getByText(readOnlyTeam.Name));
    await screen.findByText("该知识库暂无资料。");
    const timeoutSpy = vi.spyOn(window, "setTimeout");
    await act(async () =>
      processing.resolve({
        processing: {
          ID: 91,
          MaterialID: parsingMaterial.ID,
          ParseGeneration: 1,
          IndexVersion: "rag-v2",
          Stage: "persist",
          Status: "done",
          Progress: { percent: 100 },
        },
      }),
    );

    expect(screen.queryByText(/阶段：persist/)).toBeNull();
    expect(timeoutSpy.mock.calls.some((call) => call[1] === 300)).toBe(false);
    expect(api.listTeamMaterials).toHaveBeenCalledTimes(2);
  });

  it("does not let an old upload completion reset or reload the newly selected team", async () => {
    const createResult = deferred<{ material: Material }>();
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [writableTeam, readOnlyTeam] });
    vi.mocked(api.listTeamMaterials).mockImplementation(async (teamID) => ({
      materials: [],
      can_write: teamID === writableTeam.ID,
    }));
    vi.mocked(api.createMaterial).mockReturnValue(createResult.promise);
    const { container } = render(<Library onOpenMaterial={vi.fn()} />);
    await screen.findByRole("button", { name: "+ 新建资料" });
    fireEvent.click(screen.getByRole("button", { name: "+ 新建资料" }));
    fireEvent.change(screen.getByPlaceholderText("标题"), { target: { value: "旧团队资料" } });
    fireEvent.change(
      screen.getByPlaceholderText("正文内容（保存后自动解析入库，可用于 AI 答疑）"),
      { target: { value: "旧团队正文" } },
    );
    const form = container.querySelector("form");
    if (!(form instanceof HTMLFormElement)) throw new Error("material form not found");
    fireEvent.submit(form);
    await waitFor(() => expect(api.createMaterial).toHaveBeenCalledOnce());

    fireEvent.click(screen.getByText(readOnlyTeam.Name));
    await screen.findByText("该知识库暂无资料。");
    await act(async () => createResult.resolve({ material: readyMaterial }));

    expect(screen.queryByText(readyMaterial.Title)).toBeNull();
    expect(screen.queryByPlaceholderText("标题")).toBeNull();
    expect(api.listTeamMaterials).toHaveBeenCalledTimes(2);
    expect(api.listTeamMaterials).toHaveBeenLastCalledWith(readOnlyTeam.ID);
  });

  it("creates a text material, resets the form, and reloads the selected team", async () => {
    vi.mocked(api.listTeamMaterials)
      .mockResolvedValueOnce({ materials: [], can_write: true })
      .mockResolvedValueOnce({ materials: [readyMaterial], can_write: true });
    const { container } = render(<Library onOpenMaterial={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "+ 新建资料" })).not.toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "+ 新建资料" }));
    fireEvent.click(screen.getByRole("button", { name: "+ 新建资料" }));
    expect(screen.queryByPlaceholderText("标题")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "+ 新建资料" }));
    fireEvent.change(screen.getByPlaceholderText("标题"), { target: { value: "牛顿定律" } });
    fireEvent.change(
      screen.getByPlaceholderText("正文内容（保存后自动解析入库，可用于 AI 答疑）"),
      {
        target: { value: "F=ma" },
      },
    );
    const form = container.querySelector("form");
    if (!(form instanceof HTMLFormElement)) throw new Error("material form not found");
    fireEvent.submit(form);

    await waitFor(() =>
      expect(api.createMaterial).toHaveBeenCalledWith({
        team_id: writableTeam.ID,
        title: "牛顿定律",
        content: "F=ma",
        file_type: "txt",
      }),
    );
    await screen.findByText(readyMaterial.Title);
    expect(screen.queryByPlaceholderText("标题")).toBeNull();
    expect(api.listTeamMaterials).toHaveBeenCalledTimes(2);
  });

  it.each([
    [new Error("知识库加载失败"), "知识库加载失败"],
    ["network", "加载失败"],
  ])("shows team loading errors", async (reason, message) => {
    vi.mocked(api.listTeams).mockRejectedValue(reason);
    render(<Library onOpenMaterial={vi.fn()} />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载知识库…")).toBeNull();
  });

  it("shows material, create, and retry fallback errors", async () => {
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [writableTeam, readOnlyTeam] });
    vi.mocked(api.listTeamMaterials).mockRejectedValueOnce("network");
    const { container } = render(<Library onOpenMaterial={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("加载失败")).not.toBeNull());

    vi.mocked(api.listTeamMaterials).mockResolvedValue({
      materials: [failedMaterial],
      can_write: true,
    });
    fireEvent.click(container.querySelectorAll<HTMLElement>(".side-item")[1]);
    await screen.findByText(failedMaterial.Title);

    vi.mocked(api.createMaterial).mockRejectedValueOnce(new Error("创建被拒绝"));
    fireEvent.click(screen.getByRole("button", { name: "+ 新建资料" }));
    const form = container.querySelector("form");
    if (!(form instanceof HTMLFormElement)) throw new Error("material form not found");
    fireEvent.submit(form);
    await waitFor(() => expect(screen.getByText("创建被拒绝")).not.toBeNull());

    vi.mocked(api.retryMaterialParse).mockRejectedValueOnce("network");
    fireEvent.click(screen.getByRole("button", { name: "重试解析" }));
    await waitFor(() => expect(screen.getByText("重试失败")).not.toBeNull());
    expect((screen.getByRole("button", { name: "重试解析" }) as HTMLButtonElement).disabled).toBe(
      false,
    );
  });
});
