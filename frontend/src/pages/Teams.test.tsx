// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type Team, type TeamMember, type User } from "../api";
import { useAuth } from "../auth";
import Teams from "./Teams";

vi.mock("../auth", () => ({ useAuth: vi.fn() }));

const teacher: User = {
  ID: 8,
  Email: "teacher@test.dev",
  DisplayName: "测试老师",
  Role: "teacher",
  Subscription: "free",
  CreatedAt: "2026-07-15T00:00:00Z",
};

const student: User = {
  ...teacher,
  ID: 7,
  Email: "student@test.dev",
  DisplayName: "测试学生",
  Role: "student",
};

const ownedTeam: Team = {
  ID: 21,
  Name: "物理学习组",
  Type: "teacher",
  JoinCode: "PHY123",
  OwnerID: teacher.ID,
  CreatedAt: "2026-07-15T00:00:00Z",
};

const otherTeam: Team = {
  ...ownedTeam,
  ID: 22,
  Name: "其他老师小组",
  JoinCode: "OTHER1",
  OwnerID: 99,
};

const pendingMember: TeamMember = {
  TeamID: ownedTeam.ID,
  UserID: student.ID,
  Role: "member",
  Status: "pending",
  JoinedAt: "2026-07-15T00:00:00Z",
};

const approvedMember: TeamMember = {
  ...pendingMember,
  UserID: 9,
  Status: "approved",
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

function setUser(user: User | null) {
  vi.mocked(useAuth).mockReturnValue({
    user,
    ready: true,
    login: vi.fn(),
    register: vi.fn(),
    logout: vi.fn(),
  });
}

describe("Teams", () => {
  beforeEach(() => {
    setUser(teacher);
    vi.spyOn(api, "listTeams").mockResolvedValue({ teams: [ownedTeam, otherTeam] });
    vi.spyOn(api, "createTeam").mockResolvedValue(ownedTeam);
    vi.spyOn(api, "joinByCode").mockResolvedValue({ status: "pending", team_id: ownedTeam.ID });
    vi.spyOn(api, "listMembers").mockResolvedValue({ members: [] });
    vi.spyOn(api, "approveMember").mockResolvedValue({ ok: true });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("keeps loading and empty states distinct and renders only the teacher action", async () => {
    const teams = deferred<{ teams: Team[] }>();
    vi.mocked(api.listTeams).mockReturnValue(teams.promise);
    render(<Teams />);

    expect(screen.getByText("正在加载团队…")).not.toBeNull();
    expect(screen.queryByText("还没有团队或知识库。")).toBeNull();
    expect(screen.getByPlaceholderText("小组名称")).not.toBeNull();
    expect(screen.queryByPlaceholderText("输入老师提供的班级码")).toBeNull();

    await act(async () => teams.resolve({ teams: [] }));
    await waitFor(() => expect(screen.getByText("还没有团队或知识库。")).not.toBeNull());
    expect(screen.queryByText("正在加载团队…")).toBeNull();
  });

  it("creates a teacher team, reloads the list, and only exposes owner controls", async () => {
    render(<Teams />);
    await screen.findByText(ownedTeam.Name);

    expect(screen.getByText(ownedTeam.JoinCode as string)).not.toBeNull();
    expect(screen.queryByText(otherTeam.JoinCode as string)).toBeNull();
    expect(screen.getAllByRole("button", { name: "管理成员 / 审批" })).toHaveLength(1);

    const input = screen.getByPlaceholderText("小组名称") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "新学习组" } });
    fireEvent.submit(requireForm(input));

    await waitFor(() => expect(api.createTeam).toHaveBeenCalledWith("新学习组"));
    expect(input.value).toBe("");
    expect(api.listTeams).toHaveBeenCalledTimes(2);
  });

  it("loads members without a false empty state, approves pending users, and closes management", async () => {
    const firstMembers = deferred<{ members: TeamMember[] }>();
    vi.mocked(api.listMembers)
      .mockReturnValueOnce(firstMembers.promise)
      .mockResolvedValueOnce({
        members: [{ ...pendingMember, Status: "approved" }, approvedMember],
      });
    render(<Teams />);
    await screen.findByText(ownedTeam.Name);

    fireEvent.click(screen.getByRole("button", { name: "管理成员 / 审批" }));
    expect(screen.getByText("正在加载成员…")).not.toBeNull();
    expect(screen.queryByText("暂无成员。")).toBeNull();

    await act(async () => firstMembers.resolve({ members: [pendingMember, approvedMember] }));
    await waitFor(() => expect(screen.getByRole("button", { name: "通过" })).not.toBeNull());
    expect(screen.getByText(/用户 #9/)).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "通过" }));
    await waitFor(() => expect(api.approveMember).toHaveBeenCalledWith(ownedTeam.ID, student.ID));
    await waitFor(() => expect(screen.queryByRole("button", { name: "通过" })).toBeNull());
    expect(api.listMembers).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.queryByText("成员审批")).toBeNull();
  });

  it("lets students join by code and reports pending and approved outcomes", async () => {
    setUser(student);
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [] });
    vi.mocked(api.joinByCode)
      .mockResolvedValueOnce({ status: "pending", team_id: ownedTeam.ID })
      .mockResolvedValueOnce({ status: "approved", team_id: ownedTeam.ID });
    render(<Teams />);
    await screen.findByText("还没有团队或知识库。");

    expect(screen.queryByPlaceholderText("小组名称")).toBeNull();
    const input = screen.getByPlaceholderText("输入老师提供的班级码") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "PHY123" } });
    fireEvent.submit(requireForm(input));
    await waitFor(() => expect(screen.getByText("已提交申请，等待老师审批。")).not.toBeNull());
    expect(input.value).toBe("");

    fireEvent.change(input, { target: { value: "PHY123" } });
    fireEvent.submit(requireForm(input));
    await waitFor(() => expect(screen.getByText("已加入。")).not.toBeNull());
    expect(api.joinByCode).toHaveBeenNthCalledWith(1, "PHY123");
    expect(api.joinByCode).toHaveBeenNthCalledWith(2, "PHY123");
    expect(api.listTeams).toHaveBeenCalledTimes(3);
  });

  it.each([
    [new Error("团队加载失败"), "团队加载失败"],
    ["network", "加载失败"],
  ])("reports team list failures", async (reason, message) => {
    vi.mocked(api.listTeams).mockRejectedValue(reason);
    render(<Teams />);

    await waitFor(() => expect(screen.getByText(message)).not.toBeNull());
    expect(screen.queryByText("正在加载团队…")).toBeNull();
  });

  it("reports create, join, member loading, and approval failures", async () => {
    vi.mocked(api.createTeam).mockRejectedValueOnce("network");
    render(<Teams />);
    await screen.findByText(ownedTeam.Name);
    const createInput = screen.getByPlaceholderText("小组名称");
    fireEvent.submit(requireForm(createInput));
    await waitFor(() => expect(screen.getByText("创建失败")).not.toBeNull());

    vi.mocked(api.listMembers).mockRejectedValueOnce("network");
    fireEvent.click(screen.getByRole("button", { name: "管理成员 / 审批" }));
    await waitFor(() => expect(screen.getByText("加载成员失败")).not.toBeNull());
    expect(screen.getByText("暂无成员。")).not.toBeNull();

    vi.mocked(api.listMembers).mockResolvedValueOnce({ members: [pendingMember] });
    fireEvent.click(screen.getByRole("button", { name: "管理成员 / 审批" }));
    await screen.findByRole("button", { name: "通过" });
    vi.mocked(api.approveMember).mockRejectedValueOnce("network");
    fireEvent.click(screen.getByRole("button", { name: "通过" }));
    await waitFor(() => expect(screen.getByText("审批失败")).not.toBeNull());

    cleanup();
    setUser(student);
    vi.mocked(api.listTeams).mockResolvedValue({ teams: [] });
    vi.mocked(api.joinByCode).mockRejectedValueOnce(new Error("班级码无效"));
    render(<Teams />);
    await screen.findByText("还没有团队或知识库。");
    const joinInput = screen.getByPlaceholderText("输入老师提供的班级码");
    fireEvent.submit(requireForm(joinInput));
    await waitFor(() => expect(screen.getByText("班级码无效")).not.toBeNull());
  });
});

function requireForm(element: Element): HTMLFormElement {
  const form = element.closest("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("form not found");
  return form;
}
