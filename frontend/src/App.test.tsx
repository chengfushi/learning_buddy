// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import App from "./App";
import type { User } from "./api";
import { useAuth } from "./auth";

vi.mock("./auth", () => ({ useAuth: vi.fn() }));
vi.mock("./pages/Login", () => ({
  default: function MockLogin() {
    return <div data-testid="login-view">login</div>;
  },
}));
vi.mock("./pages/Library", () => ({
  default: function MockLibrary({ onOpenMaterial }: { onOpenMaterial: (id: number) => void }) {
    return (
      <div data-testid="library-view">
        library
        <button type="button" onClick={() => onOpenMaterial(42)}>
          open material
        </button>
      </div>
    );
  },
}));
vi.mock("./pages/Reader", () => ({
  default: function MockReader({
    materialId,
    focus,
    onBack,
    onAsk,
  }: {
    materialId: number;
    focus?: { pageNumber?: number; assetId?: number };
    onBack: () => void;
    onAsk: (id: number) => void;
  }) {
    return (
      <div data-testid="reader-view">
        reader-{materialId}-page-{focus?.pageNumber ?? "none"}-asset-{focus?.assetId ?? "none"}
        <button type="button" onClick={onBack}>
          back
        </button>
        <button type="button" onClick={() => onAsk(materialId)}>
          ask
        </button>
      </div>
    );
  },
}));
vi.mock("./pages/Teams", () => ({
  default: function MockTeams() {
    return <div data-testid="teams-view">teams</div>;
  },
}));
vi.mock("./pages/Companion", () => ({
  default: function MockCompanion({
    materialId,
    onOpenMaterial,
  }: {
    materialId?: number;
    onOpenMaterial?: (target: {
      materialId: number;
      pageNumber?: number;
      assetId?: number;
    }) => void;
  }) {
    return (
      <div data-testid="companion-view">
        companion-{materialId ?? "none"}
        <button
          type="button"
          onClick={() => onOpenMaterial?.({ materialId: 99, pageNumber: 4, assetId: 8 })}
        >
          open citation
        </button>
      </div>
    );
  },
}));
vi.mock("./pages/Learning", () => ({
  default: function MockLearning() {
    return <div data-testid="learning-view">learning</div>;
  },
}));

const user: User = {
  ID: 7,
  Email: "student@test.dev",
  DisplayName: "测试学生",
  Role: "student",
  Subscription: "free",
  CreatedAt: "2026-07-15T00:00:00Z",
};

const logout = vi.fn();

function setAuth(value: { user: User | null; ready: boolean }) {
  vi.mocked(useAuth).mockReturnValue({
    ...value,
    login: vi.fn(),
    register: vi.fn(),
    logout,
  });
}

describe("App", () => {
  beforeEach(() => {
    logout.mockReset();
    setAuth({ user, ready: true });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows a loading state before authentication is ready", () => {
    setAuth({ user: null, ready: false });

    render(<App />);

    expect(screen.getByText("加载中…").className).toBe("loading");
    expect(screen.queryByTestId("login-view")).toBeNull();
  });

  it("protects the application shell from unauthenticated users", () => {
    setAuth({ user: null, ready: true });

    render(<App />);

    expect(screen.getByTestId("login-view")).not.toBeNull();
    expect(screen.queryByTestId("library-view")).toBeNull();
  });

  it("renders the user shell, switches all primary views, and logs out", () => {
    render(<App />);

    expect(screen.getByText("测试学生（student）")).not.toBeNull();
    expect(screen.getByTestId("library-view")).not.toBeNull();
    expect(screen.getByRole("button", { name: "知识库" }).className).toBe("nav-on");

    fireEvent.click(screen.getByRole("button", { name: "团队" }));
    expect(screen.getByTestId("teams-view")).not.toBeNull();
    expect(screen.getByRole("button", { name: "团队" }).className).toBe("nav-on");

    fireEvent.click(screen.getByRole("button", { name: "AI 学伴" }));
    expect(screen.getByTestId("companion-view").textContent).toContain("companion-none");

    fireEvent.click(screen.getByRole("button", { name: "学习中心" }));
    expect(screen.getByTestId("learning-view")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "知识库" }));
    expect(screen.getByTestId("library-view")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "退出" }));
    expect(logout).toHaveBeenCalledOnce();
  });

  it("opens a reader, returns to the library, and starts a material-scoped question", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "open material" }));
    expect(screen.getByTestId("reader-view").textContent).toContain("reader-42");
    expect(screen.queryByTestId("library-view")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "back" }));
    expect(screen.getByTestId("library-view")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "open material" }));
    fireEvent.click(screen.getByRole("button", { name: "ask" }));

    expect(screen.getByTestId("companion-view").textContent).toContain("companion-42");
    expect(screen.queryByTestId("reader-view")).toBeNull();
    expect(screen.getByRole("button", { name: "AI 学伴" }).className).toBe("nav-on");
  });

  it("opens a citation at its page and image location", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "AI 学伴" }));
    fireEvent.click(screen.getByRole("button", { name: "open citation" }));

    expect(screen.getByTestId("reader-view").textContent).toContain("reader-99-page-4-asset-8");
  });
});
