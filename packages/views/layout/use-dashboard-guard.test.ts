import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Workspace } from "@multica/core/types";
import { useDashboardGuard } from "./use-dashboard-guard";

const state = vi.hoisted(() => ({
  desktop: false,
  pathname: "/company/issues",
  workspaces: [] as Workspace[],
  current: null as Workspace | null,
  onboarded: true,
  replace: vi.fn(),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: unknown; isLoading: boolean }) => unknown) =>
    selector({
      user: {
        id: "user-1",
        name: "Ada",
        email: "ada@example.com",
        onboarded_at: "2026-01-01T00:00:00Z",
      },
      isLoading: false,
    }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => state.current,
  useHasOnboarded: () => state.onboarded,
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ workspaces: state.workspaces, ready: true }),
}));

vi.mock("@multica/core/navigation", () => ({
  useNavigationStore: { getState: () => ({ onPathChange: vi.fn() }) },
}));

vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: { getState: () => ({ pruneWorkspaces: vi.fn() }) },
}));

vi.mock("@multica/core/command-bar", () => ({
  useRecentVisitsStore: { getState: () => ({ pruneWorkspaces: vi.fn() }) },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: state.pathname, replace: state.replace }),
}));

vi.mock("../platform", () => ({
  isDesktopShell: () => state.desktop,
}));

const workspace = (overrides: Partial<Workspace> = {}): Workspace => ({
  id: "ws-company",
  name: "Company",
  slug: "company",
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "COM",
  avatar_url: null,
  is_organization: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

beforeEach(() => {
  state.desktop = false;
  state.pathname = "/company/issues";
  state.onboarded = true;
  state.replace.mockClear();
  state.workspaces = [workspace()];
  state.current = state.workspaces[0]!;
});

describe("useDashboardGuard", () => {
  it("sends an ordinary member without management capability to the C-end", async () => {
    state.workspaces = [workspace({ viewer_can_manage: false })];
    state.current = state.workspaces[0]!;
    renderHook(() => useDashboardGuard());
    await waitFor(() => expect(state.replace).toHaveBeenCalledWith("/home"));
  });

  // Older servers omit the field entirely; that must keep the legacy access
  // instead of bouncing every department lead to the C-end.
  it("keeps the admin side when the server does not report the capability", async () => {
    state.workspaces = [workspace()];
    state.current = state.workspaces[0]!;
    renderHook(() => useDashboardGuard());
    await waitFor(() => expect(state.replace).not.toHaveBeenCalled());
  });

  it("keeps the admin side for a department lead", async () => {
    state.workspaces = [workspace({ viewer_can_manage: true })];
    state.current = state.workspaces[0]!;
    renderHook(() => useDashboardGuard());
    await waitFor(() => expect(state.replace).not.toHaveBeenCalled());
  });

  it("leaves desktop on the workspace shell", async () => {
    state.desktop = true;
    state.workspaces = [workspace({ viewer_can_manage: false })];
    state.current = state.workspaces[0]!;
    renderHook(() => useDashboardGuard());
    await waitFor(() => expect(state.replace).not.toHaveBeenCalled());
  });

  it("lands web on the C-end when the slug resolves to no workspace", async () => {
    state.current = null;
    renderHook(() => useDashboardGuard());
    await waitFor(() => expect(state.replace).toHaveBeenCalledWith("/home"));
  });

  it("lands desktop on its first workspace when the slug resolves to none", async () => {
    state.desktop = true;
    state.workspaces = [workspace({ slug: "first" })];
    state.current = null;
    renderHook(() => useDashboardGuard());
    await waitFor(() =>
      expect(state.replace).toHaveBeenCalledWith("/first/workbench"),
    );
  });
});
