// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, getToken, setToken, type User } from "./api";
import { AuthProvider, useAuth } from "./auth";

const student: User = {
  ID: 7,
  Email: "student@test.dev",
  DisplayName: "测试学生",
  Role: "student",
  Subscription: "free",
  CreatedAt: "2026-07-15T00:00:00Z",
};

const teacher: User = {
  ...student,
  ID: 8,
  Email: "teacher@test.dev",
  DisplayName: "测试老师",
  Role: "teacher",
};

function AuthProbe() {
  const { user, ready, login, register, logout } = useAuth();
  return (
    <div>
      <span data-testid="ready">{ready ? "ready" : "loading"}</span>
      <span data-testid="user">{user?.Email ?? "guest"}</span>
      <button type="button" onClick={() => void login(student.Email, "password123")}>
        login
      </button>
      <button
        type="button"
        onClick={() =>
          void register(teacher.Email, "password123", teacher.DisplayName, teacher.Role)
        }
      >
        register
      </button>
      <button type="button" onClick={logout}>
        logout
      </button>
    </div>
  );
}

function renderProvider() {
  return render(
    <AuthProvider>
      <AuthProbe />
    </AuthProvider>,
  );
}

function createMemoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear() {
      values.clear();
    },
    getItem(key: string) {
      return values.get(key) ?? null;
    },
    key(index: number) {
      return [...values.keys()][index] ?? null;
    },
    removeItem(key: string) {
      values.delete(key);
    },
    setItem(key: string, value: string) {
      values.set(key, value);
    },
  };
}

describe("AuthProvider", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", createMemoryStorage());
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("becomes ready without calling /me when no token exists", async () => {
    const me = vi.spyOn(api, "me");

    renderProvider();

    await waitFor(() => expect(screen.getByTestId("ready").textContent).toBe("ready"));
    expect(screen.getByTestId("user").textContent).toBe("guest");
    expect(me).not.toHaveBeenCalled();
  });

  it("restores the current user from an existing token", async () => {
    setToken("existing-token");
    const me = vi.spyOn(api, "me").mockResolvedValue({ user: student });

    renderProvider();

    expect(screen.getByTestId("ready").textContent).toBe("loading");
    await waitFor(() => expect(screen.getByTestId("user").textContent).toBe(student.Email));
    expect(screen.getByTestId("ready").textContent).toBe("ready");
    expect(me).toHaveBeenCalledOnce();
    expect(getToken()).toBe("existing-token");
  });

  it("clears an invalid persisted token and remains logged out", async () => {
    setToken("expired-token");
    vi.spyOn(api, "me").mockRejectedValue(new Error("登录已失效"));

    renderProvider();

    await waitFor(() => expect(screen.getByTestId("ready").textContent).toBe("ready"));
    expect(screen.getByTestId("user").textContent).toBe("guest");
    expect(getToken()).toBeNull();
  });

  it("stores the access token and user after login", async () => {
    const login = vi.spyOn(api, "login").mockResolvedValue({
      user: student,
      access_token: "login-token",
    });
    renderProvider();
    await waitFor(() => expect(screen.getByTestId("ready").textContent).toBe("ready"));

    fireEvent.click(screen.getByRole("button", { name: "login" }));

    await waitFor(() => expect(screen.getByTestId("user").textContent).toBe(student.Email));
    expect(login).toHaveBeenCalledWith(student.Email, "password123");
    expect(getToken()).toBe("login-token");
  });

  it("stores the access token and user after registration", async () => {
    const register = vi.spyOn(api, "register").mockResolvedValue({
      user: teacher,
      access_token: "register-token",
    });
    renderProvider();
    await waitFor(() => expect(screen.getByTestId("ready").textContent).toBe("ready"));

    fireEvent.click(screen.getByRole("button", { name: "register" }));

    await waitFor(() => expect(screen.getByTestId("user").textContent).toBe(teacher.Email));
    expect(register).toHaveBeenCalledWith(
      teacher.Email,
      "password123",
      teacher.DisplayName,
      teacher.Role,
    );
    expect(getToken()).toBe("register-token");
  });

  it("clears both token and user on logout", async () => {
    setToken("existing-token");
    vi.spyOn(api, "me").mockResolvedValue({ user: student });
    renderProvider();
    await waitFor(() => expect(screen.getByTestId("user").textContent).toBe(student.Email));

    fireEvent.click(screen.getByRole("button", { name: "logout" }));

    expect(screen.getByTestId("user").textContent).toBe("guest");
    expect(getToken()).toBeNull();
  });

  it("rejects useAuth outside AuthProvider", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);

    expect(() => render(<AuthProbe />)).toThrow("useAuth must be used within AuthProvider");

    expect(consoleError).toHaveBeenCalled();
  });
});
