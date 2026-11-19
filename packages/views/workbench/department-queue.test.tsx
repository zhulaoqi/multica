import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { DispatchQueueItem } from "@multica/core/workbench";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { DepartmentQueue } from "./department-queue";

const state = vi.hoisted(() => ({
  dispatches: [] as DispatchQueueItem[],
  isLoading: false,
  error: null as Error | null,
  push: vi.fn(),
}));

vi.mock("@multica/core/workbench", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/workbench")>();
  return {
    ...actual,
    useDepartmentDispatches: () => ({
      dispatches: state.dispatches,
      isLoading: state.isLoading,
      error: state.error,
    }),
  };
});

const adapter: NavigationAdapter = {
  push: (...args) => state.push(...args),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/workbench",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function queueItem(overrides: Partial<DispatchQueueItem> = {}): DispatchQueueItem {
  return {
    issue: {
      id: "issue-1",
      identifier: "ACM-1",
      title: "修复导出",
      status: "in_progress",
    },
    unit_id: "unit-1",
    unit_path: "研发部 / 平台组",
    squad_id: "",
    personal: false,
    dispatched_at: "2026-11-12T08:00:00Z",
    completed: false,
    leader_state: "running",
    requester_id: "user-1",
    requester_name: "Henry",
    stage: "in_progress",
    blocker: null,
    ...overrides,
  } as DispatchQueueItem;
}

function render() {
  renderWithI18n(
    <WorkspaceSlugProvider slug="acme">
      <NavigationProvider value={adapter}>
        <DepartmentQueue wsId="ws-test" />
      </NavigationProvider>
    </WorkspaceSlugProvider>,
  );
}

describe("DepartmentQueue", () => {
  beforeEach(() => {
    state.dispatches = [];
    state.isLoading = false;
    state.error = null;
    state.push.mockReset();
  });

  it("renders nothing when the caller leads no unit with open work", () => {
    render();
    expect(screen.queryByRole("region", { name: "Department queue" })).toBeNull();
  });

  it("groups items by queue and shows requester and blocker", () => {
    state.dispatches = [
      queueItem({
        issue: { ...queueItem().issue, id: "issue-p", title: "等待承接" },
        stage: "pending",
        blocker: null,
      }),
      queueItem({ blocker: { done: "一半", missing: "数据库权限", consequence: "无法验收" } }),
    ];
    render();

    expect(screen.getByText("Waiting")).toBeTruthy();
    expect(screen.getByText("In progress")).toBeTruthy();
    expect(screen.getByText("等待承接")).toBeTruthy();
    expect(screen.getByText("修复导出")).toBeTruthy();
    expect(screen.getAllByText(/From Henry/)).toHaveLength(2);
    expect(screen.getByText("Blocked: 数据库权限")).toBeTruthy();
  });

  it("opens the dispatched issue", () => {
    state.dispatches = [queueItem()];
    render();

    fireEvent.click(screen.getByText("修复导出"));
    expect(state.push).toHaveBeenCalledWith("/acme/issues/issue-1");
  });
});
