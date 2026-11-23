import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { childIssuesOptions, issueTasksOptions } from "@multica/core/issues/queries";
import type { AgentTask, Issue } from "@multica/core/types";
import type { DispatchListItem } from "@multica/core/workbench";
import { renderWithI18n } from "../test/i18n";
import { DispatchTrail } from "./dispatch-trail";
import type { DispatchPhase } from "./use-workbench-conversation";

const apiState = vi.hoisted(() => ({
  listChildIssues: vi.fn().mockResolvedValue({ issues: [] }),
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  listMembers: vi.fn().mockResolvedValue([]),
  listAgents: vi.fn().mockResolvedValue([]),
  listSquads: vi.fn().mockResolvedValue([]),
  listIssueStatuses: vi.fn().mockResolvedValue([]),
  getDispatchTrail: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: apiState, getApi: () => apiState };
});

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ "agent-1": "Mika", "agent-2": "Nova" })[id] ?? id,
  }),
}));

vi.mock("../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => `status:${status}`,
}));

// The delivery block reads the issue timeline and its files; they are their own
// components with their own tests, so this suite only checks that delivery
// shows them.
vi.mock("./dispatch-result-block", () => ({
  DispatchResultBlock: ({ result }: { result?: { content: string } | null }) => (
    <p>{result ? `trail result: ${result.content}` : "result body"}</p>
  ),
}));
vi.mock("./dispatch-deliverables", () => ({
  DispatchDeliverables: ({
    deliverables,
  }: {
    deliverables?: { filename: string }[];
  }) => (
    <p>
      {deliverables
        ? `trail files: ${deliverables.map((file) => file.filename).join(", ")}`
        : "delivered files"}
    </p>
  ),
}));

const WS = "ws-test";
const NOW = "2026-11-12T08:00:00Z";
const LATER = "2026-11-12T09:00:00Z";

const item: DispatchListItem = {
  issue: {
    id: "issue-1",
    workspace_id: WS,
    identifier: "ACM-1",
    title: "Prepare onboarding materials",
    status: "in_progress",
    status_category: "started",
    created_at: NOW,
  },
  unit_id: "unit-1",
  unit_path: "Engineering / Customer Success",
  dispatched_at: NOW,
  completed: false,
  leader_state: "running",
} as unknown as DispatchListItem;

function child(id: string, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    identifier: `ACM-${id}`,
    title: `Task ${id}`,
    status: "in_progress",
    status_category: "started",
    parent_issue_id: "issue-1",
    assignee_type: "agent",
    assignee_id: "agent-1",
    created_at: NOW,
    ...overrides,
  } as unknown as Issue;
}

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    issue_id: "child-1",
    status: "running",
    dispatched_at: NOW,
    started_at: NOW,
    completed_at: null,
    ...overrides,
  } as unknown as AgentTask;
}

let qc: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
});

function renderTrail({
  phase,
  progress,
  children = [],
  tasks = [],
}: {
  phase: DispatchPhase;
  progress?: { done: number; total: number };
  children?: Issue[];
  tasks?: AgentTask[];
}) {
  qc.setQueryData(childIssuesOptions(WS, "issue-1").queryKey, children);
  for (const entry of children) {
    qc.setQueryData(
      issueTasksOptions(entry.id).queryKey,
      tasks.filter((run) => run.issue_id === entry.id),
    );
  }
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <DispatchTrail wsId={WS} item={item} phase={phase} progress={progress} />
    </QueryClientProvider>,
  );
}

const remoteItem: DispatchListItem = {
  ...item,
  issue: { ...item.issue, workspace_id: "ws-project", identifier: "AIHUB-7" },
};

function trailChild(overrides: Record<string, unknown> = {}) {
  return {
    id: "child-1",
    title: "Task child-1",
    status: "in_progress",
    status_category: "started",
    status_label: "In progress",
    assignee_type: "agent",
    assignee_id: "agent-1",
    assignee_name: "Mika",
    created_at: NOW,
    started_at: NOW,
    completed_at: null,
    ...overrides,
  };
}

function trail(overrides: Record<string, unknown> = {}) {
  return {
    issue: {
      id: "issue-1",
      workspace_id: "ws-project",
      identifier: "AIHUB-7",
      title: "Which models does AIHUB serve?",
      status: "in_progress",
      status_category: "started",
      created_at: NOW,
    },
    assignee_name: "AIHUB agent",
    children: [],
    progress: { done: 0, total: 0 },
    result: null,
    deliverables: [],
    ...overrides,
  };
}

function renderRemoteTrail(phase: DispatchPhase) {
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <DispatchTrail wsId={WS} item={remoteItem} phase={phase} />
    </QueryClientProvider>,
  );
}

describe("DispatchTrail", () => {
  it("shows where the request went, the plan, and who is on each task", () => {
    renderTrail({
      phase: "progress",
      progress: { done: 1, total: 2 },
      children: [
        child("child-1", { title: "Collect the source docs" }),
        child("child-2", {
          title: "Draft the outline",
          status: "done",
          status_category: "done",
          assignee_id: "agent-2",
        }),
      ],
      tasks: [
        task(),
        task({
          id: "task-2",
          agent_id: "agent-2",
          issue_id: "child-2",
          status: "completed",
          completed_at: LATER,
        }),
      ],
    });

    const trail = screen.getByRole("region", { name: "How it's going" });
    expect(trail).toHaveTextContent("Sent to Engineering / Customer Success");
    expect(trail).toHaveTextContent("Broke it into 2 tasks");
    expect(trail).toHaveTextContent("1 of 2 tasks done");
    expect(trail).toHaveTextContent("Collect the source docs");
    expect(trail).toHaveTextContent("Draft the outline");
    expect(trail).toHaveTextContent("Mika");
    expect(trail).toHaveTextContent("Nova");
    expect(trail).toHaveTextContent("status:in_progress");
    expect(trail).toHaveTextContent("Waiting for your review");
  });

  it("shows the delivery once the department hands the work back", () => {
    renderTrail({ phase: "in_review" });

    const trail = screen.getByRole("region", { name: "How it's going" });
    expect(trail).toHaveTextContent("Delivered");
    expect(trail).toHaveTextContent("result body");
    expect(trail).toHaveTextContent("delivered files");
    expect(trail).not.toHaveTextContent("Waiting for your review");
  });

  it("marks accepted work as accepted", () => {
    renderTrail({ phase: "completed" });

    const trail = screen.getByRole("region", { name: "How it's going" });
    expect(trail).toHaveTextContent("Accepted");
    expect(trail).not.toHaveTextContent("Waiting for your review");
  });

  it("says a stopped request never started", () => {
    renderTrail({ phase: "failed" });

    const trail = screen.getByRole("region", { name: "How it's going" });
    expect(trail).toHaveTextContent("Stopped before it started");
  });

  it("does not read a dispatch delivered here through the trail endpoint", () => {
    renderTrail({ phase: "progress", children: [child("child-1")], tasks: [task()] });

    expect(screen.getByText("Task child-1")).toBeInTheDocument();
    expect(apiState.getDispatchTrail).not.toHaveBeenCalled();
  });

  it("reads a dispatch delivered into another workspace through the trail", async () => {
    apiState.getDispatchTrail.mockResolvedValue(
      trail({
        children: [
          trailChild({
            id: "child-9",
            title: "Check the gateway config",
            status: "done",
            status_category: "done",
            status_label: "Shipped",
            assignee_name: "Mika",
            started_at: NOW,
            completed_at: LATER,
          }),
        ],
        progress: { done: 1, total: 3 },
      }),
    );
    renderRemoteTrail("progress");

    const trailRegion = screen.getByRole("region", { name: "How it's going" });
    expect(await screen.findByText("Check the gateway config")).toBeInTheDocument();
    expect(trailRegion).toHaveTextContent("Mika");
    // The label comes from the issue's own workspace, not this one's catalog.
    expect(trailRegion).toHaveTextContent("Shipped");
    expect(trailRegion).toHaveTextContent("1 of 3 tasks done");
    expect(apiState.getDispatchTrail).toHaveBeenCalledWith("issue-1");
    // The project workspace's issues are unreadable from here.
    expect(apiState.listChildIssues).not.toHaveBeenCalled();
    expect(apiState.listTasksByIssue).not.toHaveBeenCalled();
  });

  it("shows the project's result and files once the trail reports delivery", async () => {
    apiState.getDispatchTrail.mockResolvedValue(
      trail({
        result: {
          actor_name: "Mika",
          content: "AIHUB routes to Claude and GPT.",
          created_at: LATER,
          attachments: [],
        },
        deliverables: [{ key: "k1", filename: "models.csv" }],
      }),
    );
    renderRemoteTrail("in_review");

    expect(
      await screen.findByText("trail result: AIHUB routes to Claude and GPT."),
    ).toBeInTheDocument();
    const trailRegion = screen.getByRole("region", { name: "How it's going" });
    expect(trailRegion).toHaveTextContent("trail files: models.csv");
    expect(trailRegion).not.toHaveTextContent("No result was posted");
  });
});
