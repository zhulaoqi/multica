import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import type { TimelineEntry } from "@multica/core/types";
import type { DispatchListItem, DispatchWorkResponse } from "@multica/core/workbench";
import {
  orgTreeOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { WorkbenchPage } from "./workbench-page";

const WS = "ws-test";
const NOW = "2026-11-12T08:00:00Z";

const state = vi.hoisted(() => ({
  dispatches: [] as DispatchListItem[],
  isLoading: false,
  error: null as Error | null,
  mutateAsync: vi.fn(),
  push: vi.fn(),
  replace: vi.fn(),
  searchParams: new URLSearchParams(),
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
    useDispatchWork: () => ({ mutateAsync: state.mutateAsync }),
  };
});

vi.mock("./use-workbench-conversation", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./use-workbench-conversation")>();
  return {
    ...actual,
    useWorkbenchConversation: () => ({
      phaseFor: (item: DispatchListItem) => actual.dispatchPhase(item, undefined),
      progressFor: () => undefined,
    }),
  };
});

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

function dispatchResponse(issueId = "issue-2"): DispatchWorkResponse {
  return {
    issue: {
      id: issueId,
      identifier: "ACM-2",
      title: "Investigate checkout failures",
      description: "Investigate checkout failures from yesterday",
      status: "todo",
      created_at: NOW,
    },
    dispatch: { unit_id: "unit-2", unit_path: "Engineering / Payments" },
  } as unknown as DispatchWorkResponse;
}

const adapter: NavigationAdapter = {
  push: (...args) => state.push(...args),
  replace: (...args) => state.replace(...args),
  back: vi.fn(),
  pathname: "/acme/workbench",
  get searchParams() {
    return state.searchParams;
  },
  hash: "",
  getShareableUrl: (path) => path,
};

let qc: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  state.dispatches = [];
  state.isLoading = false;
  state.error = null;
  state.searchParams = new URLSearchParams();
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
  qc.setQueryData(orgTreeOptions(WS).queryKey, {
    units: [
      {
        id: "unit-1",
        parent_id: "",
        name: "Engineering",
        lead_user_id: "",
        can_manage: false,
        description: "",
        position: 0,
      },
      {
        id: "unit-2",
        parent_id: "unit-1",
        name: "Customer Success",
        lead_user_id: "",
        can_manage: false,
        description: "",
        position: 0,
      },
    ],
    agents: [],
    members: [],
    squads: [],
    can_manage: false,
    my_unit_id: "",
  });
});

function renderPage() {
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <WorkbenchPage />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

async function submitText(text: string) {
  const input = screen.getByRole("textbox", { name: "What needs doing?" });
  await userEvent.type(input, text);
  fireEvent.keyDown(input, { key: "Enter" });
}

describe("WorkbenchPage", () => {
  it("invites a first request with examples that fill the composer", async () => {
    renderPage();

    expect(screen.getByText("Tell the team what needs doing")).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Investigate yesterday's checkout failures" }),
    );

    expect(screen.getByRole("textbox", { name: "What needs doing?" })).toHaveValue(
      "Investigate yesterday's checkout failures",
    );
  });

  it("sends the trimmed text and opens the new thread", async () => {
    state.mutateAsync.mockResolvedValue(dispatchResponse());
    renderPage();

    await submitText("  Investigate checkout failures  ");

    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          text: "Investigate checkout failures",
          unitId: undefined,
        }),
      ),
    );
    await waitFor(() =>
      expect(state.replace).toHaveBeenCalledWith("/acme/workbench?dispatch=issue-2"),
    );
    expect(screen.getByRole("textbox", { name: "What needs doing?" })).toHaveValue("");
  });

  it("keeps Shift+Enter as a newline instead of sending", async () => {
    renderPage();
    const input = screen.getByRole("textbox", { name: "What needs doing?" });
    await userEvent.type(input, "First line");
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });

    expect(state.mutateAsync).not.toHaveBeenCalled();
  });

  it("keeps the text and offers a retry when dispatch fails", async () => {
    state.mutateAsync.mockRejectedValue(
      new ApiError("unroutable", 422, "Unprocessable Entity", {
        code: "org_dispatch_unroutable",
      }),
    );
    renderPage();

    await submitText("Investigate checkout failures");

    expect(
      await screen.findByText(
        "Couldn't tell which department should take this. Name a department or add more detail.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Investigate checkout failures", { selector: "p" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send again" })).toBeInTheDocument();
  });

  it("resends with a department after the requester picks a candidate", async () => {
    state.mutateAsync.mockRejectedValueOnce(
      new ApiError("ambiguous", 422, "Unprocessable Entity", {
        code: "org_dispatch_ambiguous",
        candidates: [
          { unit_id: "unit-pay", path: "Engineering / Payments", agent_count: 3 },
          { unit_id: "unit-infra", path: "Engineering / Infrastructure", agent_count: 1 },
        ],
      }),
    );
    state.mutateAsync.mockResolvedValueOnce(dispatchResponse());
    renderPage();

    await submitText("Investigate checkout failures");

    expect(
      await screen.findByText("Several departments could take this. Pick one below."),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: /Engineering \/ Payments/ }),
    );

    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenLastCalledWith(
        expect.objectContaining({
          text: "Investigate checkout failures",
          unitId: "unit-pay",
        }),
      ),
    );
  });

  it("renders the selected history entry as a conversation", () => {
    state.dispatches = [dispatchItem()];
    state.searchParams = new URLSearchParams("dispatch=issue-1");
    qc.setQueryData(issueTimelineOptions("issue-1").queryKey, []);
    renderPage();

    expect(screen.getByText("Prepare onboarding materials for next week")).toBeInTheDocument();
    expect(screen.getByText("Delivered. Waiting for your review.")).toBeInTheDocument();
    expect(screen.getAllByText("Engineering / Customer Success").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "ACM-1" })).toBeInTheDocument();
  });

  it("shows the department's delivered result on a finished dispatch", () => {
    state.dispatches = [
      dispatchItem({ completed: true, issue: { ...dispatchItem().issue, status: "done", status_category: "done" } }),
    ];
    state.searchParams = new URLSearchParams("dispatch=issue-1");
    const comment = (overrides: Partial<TimelineEntry>): TimelineEntry => ({
      type: "comment",
      id: "c",
      actor_type: "agent",
      actor_id: "agent-1",
      created_at: NOW,
      comment_type: "comment",
      ...overrides,
    });
    qc.setQueryData(issueTimelineOptions("issue-1").queryKey, [
      comment({ id: "c1", content: "Draft outline posted" }),
      comment({ id: "c2", actor_name: "Lead", content: "Onboarding deck is ready" }),
      comment({ id: "c3", comment_type: "system", content: "task cancelled by server" }),
    ]);
    renderPage();

    expect(screen.getByText("Completed.")).toBeInTheDocument();
    const result = screen.getByRole("region", { name: "Result" });
    expect(result).toHaveTextContent("Result from Lead");
    expect(result).toHaveTextContent("Onboarding deck is ready");
    expect(result).not.toHaveTextContent("task cancelled by server");
  });

  it("says so when a delivered dispatch has no result comment", () => {
    state.dispatches = [dispatchItem()];
    state.searchParams = new URLSearchParams("dispatch=issue-1");
    qc.setQueryData(issueTimelineOptions("issue-1").queryKey, []);
    renderPage();

    expect(
      screen.getByText(
        "No result was posted on the issue. Open the issue to see what was delivered.",
      ),
    ).toBeInTheDocument();
  });

  it("shows an empty history message when no work has been sent", () => {
    renderPage();
    expect(screen.getByText("Work you send will appear here.")).toBeInTheDocument();
  });

  it("drops a stale dispatch selection from the URL", async () => {
    state.searchParams = new URLSearchParams("dispatch=deleted-issue");
    renderPage();

    await waitFor(() => expect(state.replace).toHaveBeenCalledWith("/acme/workbench"));
  });

  it("preselects a department from the URL and lets the requester clear it", async () => {
    state.searchParams = new URLSearchParams("unit=unit-2");
    renderPage();

    expect(await screen.findByText("Engineering/Customer Success")).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Choose the department automatically" }),
    );
    expect(state.replace).toHaveBeenCalledWith("/acme/workbench");
  });

  it("sends to the department preselected in the URL", async () => {
    state.mutateAsync.mockResolvedValue(dispatchResponse());
    state.searchParams = new URLSearchParams("unit=unit-2");
    renderPage();

    await submitText("Investigate checkout failures");

    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ text: "Investigate checkout failures", unitId: "unit-2" }),
      ),
    );
  });
});
