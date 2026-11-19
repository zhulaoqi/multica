import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import type { OrgTree } from "@multica/core/api";
import type { DispatchListItem } from "@multica/core/workbench";
import { renderWithI18n } from "../test/i18n";
import { ReassignDispatchDialog } from "./reassign-dispatch";

const state = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
  error: null as unknown,
  tree: {
    units: [
      { id: "unit-1", parent_id: "", name: "研发部" },
      { id: "unit-2", parent_id: "unit-1", name: "平台组" },
      { id: "unit-3", parent_id: "", name: "市场部" },
    ],
    agents: [
      { id: "agent-1", name: "平台分身", unit_id: "unit-2" },
    ],
    members: [],
    squads: [],
  },
}));

vi.mock("@multica/core/workspace/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/workspace/queries")>();
  return {
    ...actual,
    orgTreeOptions: () => ({
      queryKey: ["test-org-tree"],
      queryFn: async () => state.tree as unknown as OrgTree,
    }),
  };
});

vi.mock("@multica/core/workbench", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/workbench")>();
  return {
    ...actual,
    useReassignDispatch: () => ({
      mutateAsync: state.mutateAsync,
      isPending: false,
      error: state.error,
      reset: vi.fn(),
    }),
  };
});

function item(): DispatchListItem {
  return {
    issue: { id: "issue-1", identifier: "ACM-1", title: "修复导出" },
    unit_id: "unit-1",
    unit_path: "研发部",
    squad_id: "squad-1",
    personal: false,
    dispatched_at: "2026-11-12T08:00:00Z",
    completed: false,
    leader_state: "running",
  } as unknown as DispatchListItem;
}

function render(open = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <ReassignDispatchDialog
        wsId="ws-test"
        item={item()}
        open={open}
        onOpenChange={vi.fn()}
      />
    </QueryClientProvider>,
  );
}

describe("ReassignDispatchDialog", () => {
  beforeEach(() => {
    state.mutateAsync.mockReset();
    state.error = null;
  });

  it("lists other departments and moves the work to the chosen one", async () => {
    state.mutateAsync.mockResolvedValue({
      issue: { id: "issue-1" },
      dispatch: { unit_id: "unit-2" },
    });
    render();

    // The current department is not offered.
    await waitFor(() => expect(screen.getByText("研发部/平台组")).toBeTruthy());
    expect(screen.queryByRole("button", { name: /^研发部$/ })).toBeNull();
    expect(screen.getByText("1 agent")).toBeTruthy();

    fireEvent.click(screen.getByText("研发部/平台组"));
    fireEvent.click(screen.getByRole("button", { name: "Move" }));

    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenCalledWith({
        issueId: "issue-1",
        unitId: "unit-2",
      }),
    );
  });

  it("keeps a department without agents unselectable", async () => {
    render();

    const market = await screen.findByText("市场部");
    const button = market.closest("button");
    expect(button?.disabled).toBe(true);
    expect(screen.getByText("No agents")).toBeTruthy();
  });

  it("explains why the server rejected the move", async () => {
    state.error = new ApiError("conflict", 409, "Conflict", {
      code: "org_dispatch_leader_offline",
    });
    render();

    await screen.findByText("研发部/平台组");
    expect(
      screen.getByText(/No agent in that department is online/),
    ).toBeTruthy();
  });
});
