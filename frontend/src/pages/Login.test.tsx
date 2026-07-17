// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAuth } from "../auth";
import Login from "./Login";

vi.mock("../auth", () => ({ useAuth: vi.fn() }));

const login = vi.fn<(email: string, password: string) => Promise<void>>();
const register =
  vi.fn<(email: string, password: string, name: string, role: string) => Promise<void>>();

describe("Login", () => {
  beforeEach(() => {
    login.mockReset().mockResolvedValue(undefined);
    register.mockReset().mockResolvedValue(undefined);
    vi.mocked(useAuth).mockReturnValue({
      user: null,
      ready: true,
      login,
      register,
      logout: vi.fn(),
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("submits email and password in login mode", async () => {
    const { container } = render(<Login />);
    expect(screen.queryByPlaceholderText("昵称")).toBeNull();

    fireEvent.change(screen.getByPlaceholderText("邮箱"), {
      target: { value: "student@test.dev" },
    });
    fireEvent.change(screen.getByPlaceholderText("密码"), {
      target: { value: "password123" },
    });
    fireEvent.submit(requireForm(container));

    await waitFor(() => expect(login).toHaveBeenCalledWith("student@test.dev", "password123"));
    expect(register).not.toHaveBeenCalled();
  });

  it("switches to register mode and submits all registration fields", async () => {
    const { container } = render(<Login />);
    fireEvent.click(screen.getByRole("button", { name: "注册" }));

    fireEvent.change(screen.getByPlaceholderText("昵称"), { target: { value: "测试老师" } });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "teacher" } });
    fireEvent.change(screen.getByPlaceholderText("邮箱"), {
      target: { value: "teacher@test.dev" },
    });
    fireEvent.change(screen.getByPlaceholderText("密码"), {
      target: { value: "password123" },
    });
    fireEvent.submit(requireForm(container));

    await waitFor(() =>
      expect(register).toHaveBeenCalledWith(
        "teacher@test.dev",
        "password123",
        "测试老师",
        "teacher",
      ),
    );
    expect(login).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "注册并登录" })).not.toBeNull();
  });

  it("returns from register mode to login mode", () => {
    render(<Login />);
    fireEvent.click(screen.getByRole("button", { name: "注册" }));
    expect(screen.queryByPlaceholderText("昵称")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(screen.queryByPlaceholderText("昵称")).toBeNull();
  });

  it("shows service errors and clears the old error before retrying", async () => {
    login.mockRejectedValueOnce(new Error("账号或密码错误")).mockRejectedValueOnce("network");
    const { container } = render(<Login />);
    const form = requireForm(container);

    fireEvent.submit(form);
    await waitFor(() =>
      expect(container.querySelector(".err")?.textContent).toBe("账号或密码错误"),
    );

    fireEvent.submit(form);
    await waitFor(() => expect(container.querySelector(".err")?.textContent).toBe("操作失败"));
    expect(login).toHaveBeenCalledTimes(2);
  });
});

function requireForm(container: HTMLElement): HTMLFormElement {
  const form = container.querySelector("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("Login form not found");
  return form;
}
