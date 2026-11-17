import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { childIssuesOptions } from "@multica/core/issues/queries";
import type { Issue, MemberWithUser } from "@multica/core/types";
import { workspaceKeys, workspaceListOptions } from "@multica/core/workspace/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { DispatchPlan } from "./dispatch-plan";

// childIssuesOptions refetches on mount, so the mock (not just the seeded
// cache) has to serve the fixtures.
const apiState = vi.hoisted(() => ({
  children: [] as unknown[],
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  const api = {
    listChildIssues: vi.fn(() => Promise.resolve({ issues: apiState.children })),
    listMembers: vi.fn().mockResolvedValue([]),
    listAgents: vi.fn().mockResolvedValue([]),
    listSquads: vi.fn().mockResolvedValue([]),
    listIssueStatuses: vi.fn().mockResolvedValue([]),
  };
  return { ...actual, api, getApi: () => api };
});

const WS = "ws-test";
const NOW = "2026-11-12T08:00:00Z";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/workbench",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function child(id: string, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    identifier: `ACM-${id}`,
    title: `Task ${id}`,
    description: null,
    status: "in_progress",
    status_category: "started",
    parent_issue_id: "issue-1",
    assignee_type: "member",
    assignee_id: "user-1",
    created_at: NOW,
    ...overrides,
  } as unknown as Issue;
}

let qc: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(workspaceListOptions().queryKey, [
    {
      id: WS,
      name: "Acme",
      slug: "acme",
      description: null,
      context: null,
      settings: {},
      repos: [],
      issue_prefix: "ACM",
      avatar_url: null,
      created_at: NOW,
      updated_at: NOW,
    },
  ]);
  qc.setQueryData<MemberWithUser[]>(workspaceKeys.members(WS), [
    {
      id: "member-1",
      workspace_id: WS,
      user_id: "user-1",
      role: "member",
      created_at: NOW,
      name: "Ada",
      email: "ada@example.com",
      avatar_url: null,
    },
  ]);
});

function renderPlan(children: Issue[]) {
  apiState.children = children;
  qc.setQueryData(childIssuesOptions(WS, "issue-1").queryKey, children);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <DispatchPlan wsId={WS} issueId="issue-1" />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("DispatchPlan", () => {
  it("renders nothing before the department has broken the work down", () => {
    renderPlan([]);
    expect(screen.queryByRole("region", { name: "Plan" })).not.toBeInTheDocument();
  });

  it("lists each task with its status and owner", () => {
    renderPlan([
      child("1", { title: "Collect the source docs", status: "in_progress" }),
      child("2", {
        title: "Draft the outline",
        status: "done",
        status_category: "done",
      }),
    ]);

    const plan = screen.getByRole("region", { name: "Plan" });
    expect(plan).toHaveTextContent("Collect the source docs");
    expect(plan).toHaveTextContent("Draft the outline");
    expect(plan).toHaveTextContent("In Progress");
    expect(plan).toHaveTextContent("Done");
    expect(plan).toHaveTextContent("Ada");
  });

  it("uses the reported progress for the done count", () => {
    renderPlan([
      child("1", { status: "todo", status_category: "unstarted" }),
      child("2", { status: "todo", status_category: "unstarted" }),
    ]);

    expect(screen.getByRole("button")).toHaveTextContent("0 of 2");
  });

  it("collapses a long plan until the requester opens it", async () => {
    renderPlan(
      ["1", "2", "3", "4", "5", "6"].map((id) =>
        child(id, { title: `Task ${id}` }),
      ),
    );

    const toggle = screen.getByRole("button");
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Task 6")).not.toBeInTheDocument();

    await userEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Task 6")).toBeInTheDocument();
  });
});
