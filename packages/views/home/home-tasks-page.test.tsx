import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DispatchListItem } from "@multica/core/workbench";
import { renderWithI18n } from "../test/i18n";
import { HomeTasksPage } from "./home-tasks-page";

const state = vi.hoisted(() => ({
  dispatches: [] as DispatchListItem[],
  isLoading: false,
  error: null as Error | null,
}));

vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/workbench", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/workbench")>();
  return {
    ...actual,
    useMyDispatches: () => ({
      dispatches: state.dispatches,
      isLoading: state.isLoading,
      error: state.error,
    }),
  };
});

const NOW = "2026-11-12T08:00:00Z";

function dispatchItem(overrides: Partial<DispatchListItem> = {}): DispatchListItem {
  return {
    issue: {
      id: "issue-1",
      identifier: "ACM-1",
      title: "Prepare onboarding materials",
      description: "Prepare onboarding materials for next week",
      status: "in_review",
      status_category: "in_review",
      created_at: NOW,
    },
    unit_id: "unit-1",
    unit_path: "Engineering / Customer Success",
    dispatched_at: NOW,
    completed: false,
    leader_state: "running",
    ...overrides,
  } as unknown as DispatchListItem;
}

beforeEach(() => {
  state.dispatches = [];
  state.isLoading = false;
  state.error = null;
});

describe("HomeTasksPage", () => {
  it("lists the caller's requests and opens one in the conversation", async () => {
    const onOpen = vi.fn();
    state.dispatches = [
      dispatchItem(),
      dispatchItem({
        completed: true,
        issue: {
          ...dispatchItem().issue,
          id: "issue-2",
          status: "done",
          status_category: "done",
          title: "Ship the pricing page",
        },
      }),
    ];
    renderWithI18n(<HomeTasksPage onOpen={onOpen} />);

    expect(screen.getByRole("heading", { name: "My tasks" })).toBeInTheDocument();
    expect(screen.getByText("Prepare onboarding materials")).toBeInTheDocument();
    expect(screen.getByText("Ship the pricing page")).toBeInTheDocument();

    const filters = screen.getByRole("group", { name: "Filter work" });
    expect(within(filters).getByRole("button", { name: /Needs review/ })).toHaveTextContent("1");
    expect(within(filters).getByRole("button", { name: /Done/ })).toHaveTextContent("1");

    await userEvent.click(screen.getByText("Prepare onboarding materials"));
    expect(onOpen).toHaveBeenCalledWith("issue-1");
  });

  it("filters the list without leaving the C-end", async () => {
    state.dispatches = [
      dispatchItem(),
      dispatchItem({
        completed: true,
        issue: {
          ...dispatchItem().issue,
          id: "issue-2",
          title: "Ship the pricing page",
          status: "done",
          status_category: "done",
        },
      }),
    ];
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);

    const filters = screen.getByRole("group", { name: "Filter work" });
    await userEvent.click(within(filters).getByRole("button", { name: /Needs review/ }));

    expect(screen.getByText("Prepare onboarding materials")).toBeInTheDocument();
    expect(screen.queryByText("Ship the pricing page")).not.toBeInTheDocument();
  });

  it("says so when nothing is waiting on the caller", async () => {
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);
    expect(screen.getByText("Work you send will appear here.")).toBeInTheDocument();
  });

  it("reports a failed load instead of pretending there is no work", () => {
    state.error = new Error("network");
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);
    expect(screen.getByText("Couldn't load recent work.")).toBeInTheDocument();
  });
});
