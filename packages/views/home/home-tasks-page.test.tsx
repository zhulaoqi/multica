import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, Issue } from "@multica/core/types";
import type { DispatchListItem } from "@multica/core/workbench";
import { renderWithI18n } from "../test/i18n";
import type { MyDispatchScope } from "../workbench/use-my-dispatch-scope";
import { HomeTasksPage } from "./home-tasks-page";

const state = vi.hoisted(() => ({
  scope: null as unknown,
}));

vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "agent" && id === "agent-1" ? "Mika" : id,
  }),
}));

vi.mock("../workbench/use-my-dispatch-scope", () => ({
  useMyDispatchScope: () => state.scope,
}));

const NOW = "2026-11-12T08:00:00Z";

function dispatchItem(
  id: string,
  title: string,
  overrides: Partial<DispatchListItem> = {},
): DispatchListItem {
  return {
    issue: {
      id,
      identifier: `ACM-${id}`,
      title,
      description: title,
      status: "in_progress",
      status_category: "started",
      assignee_type: null,
      assignee_id: null,
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

function withStatus(
  item: DispatchListItem,
  status: string,
  category: string,
): DispatchListItem {
  return { ...item, issue: { ...item.issue, status, status_category: category } };
}

function childIssue(id: string, assigneeId: string): Issue {
  return {
    id,
    identifier: `ACM-${id}`,
    title: `Part ${id}`,
    status: "in_progress",
    status_category: "started",
    assignee_type: "agent",
    assignee_id: assigneeId,
    created_at: NOW,
  } as unknown as Issue;
}

function scope(overrides: Partial<MyDispatchScope> = {}): MyDispatchScope {
  return {
    dispatches: [],
    isLoading: false,
    error: null,
    childrenByRoot: new Map(),
    issueIds: new Set(),
    agentIds: new Set(),
    workingAgentIds: new Set(),
    tasksByIssue: new Map(),
    progressByRoot: new Map(),
    ...overrides,
  };
}

beforeEach(() => {
  state.scope = scope();
});

describe("HomeTasksPage", () => {
  it("puts each request in its lane and opens one in the conversation", async () => {
    const onOpen = vi.fn();
    const active = dispatchItem("active", "Draft the launch plan");
    const review = withStatus(
      dispatchItem("review", "Prepare onboarding materials"),
      "in_review",
      "in_review",
    );
    const done = withStatus(
      dispatchItem("done", "Ship the pricing page", { completed: true }),
      "done",
      "done",
    );
    state.scope = scope({
      dispatches: [active, review, done],
      progressByRoot: new Map([["active", { done: 1, total: 3 }]]),
    });
    renderWithI18n(<HomeTasksPage onOpen={onOpen} />);

    expect(screen.getByRole("heading", { name: "My tasks" })).toBeInTheDocument();

    const moving = screen.getByRole("region", { name: "In progress" });
    expect(within(moving).getByText("Draft the launch plan")).toBeInTheDocument();
    expect(within(moving).getByText("1 of 3 tasks done")).toBeInTheDocument();

    const waiting = screen.getByRole("region", { name: "Awaiting review" });
    expect(within(waiting).getByText("Prepare onboarding materials")).toBeInTheDocument();

    const settled = screen.getByRole("region", { name: "Settled" });
    expect(within(settled).getByText("Ship the pricing page")).toBeInTheDocument();

    await userEvent.click(screen.getByText("Draft the launch plan"));
    expect(onOpen).toHaveBeenCalledWith("active");
  });

  it("names the agents the lead handed parts of the work to", () => {
    const active = dispatchItem("active", "Draft the launch plan");
    state.scope = scope({
      dispatches: [active],
      childrenByRoot: new Map([["active", [childIssue("active-child", "agent-1")]]]),
      tasksByIssue: new Map([
        [
          "active",
          [{ agent_id: "agent-1", status: "running", issue_id: "active" } as AgentTask],
        ],
      ]),
    });
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);

    const moving = screen.getByRole("region", { name: "In progress" });
    expect(within(moving).getByText("Mika")).toBeInTheDocument();
  });

  it("says so when nothing has been sent", () => {
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);
    expect(screen.getByText("Work you send will appear here.")).toBeInTheDocument();
  });

  it("reports a failed load instead of pretending there is no work", () => {
    state.scope = scope({ error: new Error("network") });
    renderWithI18n(<HomeTasksPage onOpen={vi.fn()} />);
    expect(screen.getByText("Couldn't load recent work.")).toBeInTheDocument();
  });
});
