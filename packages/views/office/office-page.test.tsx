import type { ReactNode, Ref } from "react";
import { forwardRef, useImperativeHandle } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, MemberWithUser } from "@multica/core/types";
import enCommon from "../locales/en/common.json";
import enOffice from "../locales/en/office.json";
import { OfficePage } from "./office-page";

const TEST_RESOURCES = { en: { common: enCommon, office: enOffice } };
const WS = "ws-test";

const room = vi.hoisted(() => ({
  peers: [] as { userId: string; x: number; y: number; dir: null }[],
  joined: true,
  error: null as string | null,
  sendMove: vi.fn(),
}));
const push = vi.hoisted(() => vi.fn());
const canvas = vi.hoisted(() => ({
  visitAgent: vi.fn(),
  playEmote: vi.fn(),
  deliverToReception: vi.fn(),
}));
const activityState = vi.hoisted(() => ({
  byAgent: new Map<string, unknown>(),
}));
const presenceState = vi.hoisted(() => ({
  byAgent: new Map<string, unknown>(),
}));
const dispatchState = vi.hoisted(() => ({
  items: [] as unknown[],
}));
const apiState = vi.hoisted(() => ({
  listSquads: vi.fn().mockResolvedValue([]),
}));
const openAgentChat = vi.hoisted(() => vi.fn());
const quickCreate = vi.hoisted(() => vi.fn());
const draftStore = vi.hoisted(() => ({
  setAgent: vi.fn(),
  setActiveMode: vi.fn(),
}));
const openModal = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: unknown) => unknown) =>
    select({ user: { id: "user-1", name: "Alex Doe" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-test" }),
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/workspace-test/agents/${id}`,
  }),
}));
const snapshot = vi.hoisted(() => ({ tasks: [] as unknown[] }));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: (wsId: string) => ({
    queryKey: ["agent-task-snapshot", wsId],
    queryFn: async () => snapshot.tasks,
  }),
  useWorkspacePresenceMap: () => ({
    byAgent: presenceState.byAgent,
    loading: false,
  }),
  useWorkspaceActivityMap: () => ({
    byAgent: activityState.byAgent,
    loading: false,
  }),
}));
vi.mock("@multica/core/workbench", () => ({
  useMyDispatches: () => ({
    dispatches: dispatchState.items,
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@multica/core/office", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/office")>();
  return { ...actual, useOfficeRoom: () => room };
});
vi.mock("@multica/core/api", () => ({
  api: { quickCreateIssue: quickCreate, listSquads: apiState.listSquads },
  ApiError: class ApiError extends Error {
    body: unknown;
    constructor(message: string, body?: unknown) {
      super(message);
      this.body = body;
    }
  },
}));
vi.mock("@multica/core/issues/stores", () => ({
  useIssueDraftStore: { getState: () => draftStore },
}));
vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: openModal }) },
}));
vi.mock("./use-open-agent-chat", () => ({
  useOpenAgentChat: () => openAgentChat,
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push }),
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));
vi.mock("../layout", () => ({
  CollectionPageHeader: ({ title }: { title: ReactNode }) => <h1>{title}</h1>,
}));
vi.mock("./components/office-canvas", () => ({
  OfficeCanvas: forwardRef(function MockOfficeCanvas(
    {
      agents,
      peers,
      companyName,
      companyLevel,
      onFocusAgentChange,
    }: {
      agents: { id: string; activity?: string | null; alert?: boolean }[];
      peers: unknown[];
      companyName: string;
      companyLevel: number;
      onFocusAgentChange: (agentId: string | null) => void;
    },
    ref: Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => canvas);
    return (
      <div data-testid="office-canvas" data-company-level={companyLevel}>
        {peers.length}
        <span data-testid="company-name">{companyName}</span>
        {agents.map((agent) => (
          <span
            key={agent.id}
            data-testid={`activity-${agent.id}`}
            data-activity={agent.activity ?? ""}
            data-alert={agent.alert ? "true" : ""}
          />
        ))}
        <button type="button" onClick={() => onFocusAgentChange("agent-1")}>
          focus-agent
        </button>
      </div>
    );
  }),
}));

function makeMember(overrides: Partial<MemberWithUser> = {}): MemberWithUser {
  return {
    id: "member-1",
    workspace_id: WS,
    user_id: "user-1",
    role: "member",
    created_at: "2026-01-01T00:00:00Z",
    name: "Alex Doe",
    email: "alex@multica.test",
    avatar_url: null,
    ...overrides,
  };
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: WS,
    runtime_id: "runtime-1",
    name: "Atlas",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "all_members",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-1",
    archived_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as unknown as Agent;
}

function activityWithCompleted(completed: number) {
  return {
    buckets: [{ total: completed, failed: 0, completed, cancelled: 0 }],
    daysSinceCreated: 20,
  };
}

function makeDispatch({
  status = "in_progress",
  leaderState = "running",
}: {
  status?: string;
  leaderState?: string;
} = {}) {
  return {
    issue: {
      id: "issue-root",
      identifier: "OPS-1",
      title: "Ship the release notes",
      description: "Write the notes",
      status,
      status_category: "in_progress",
      assignee_type: "squad",
      assignee_id: "squad-1",
    },
    unit_id: "unit-1",
    unit_path: "Engineering",
    dispatched_at: "2026-11-17T00:00:00Z",
    completed: false,
    leader_state: leaderState,
  };
}

const SQUAD = { id: "squad-1", leader_id: "agent-1", archived_at: null };

function renderPage({
  members = [makeMember(), makeMember({ id: "member-2", user_id: "user-2", name: "Jamie Chen" })],
  agents = [makeAgent()],
  squads = [],
}: {
  members?: MemberWithUser[];
  agents?: Agent[];
  squads?: unknown[];
} = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(memberListOptions(WS).queryKey, members);
  qc.setQueryData(agentListOptions(WS).queryKey, agents);
  qc.setQueryData(["agent-task-snapshot", WS], snapshot.tasks);
  qc.setQueryData(squadListOptions(WS).queryKey, squads as never);
  qc.setQueryData(issueDetailOptions(WS, "issue-11").queryKey, {
    id: "issue-11",
    identifier: "MAKE-11",
    title: "Write the launch post",
  } as never);
  const ui = () => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <OfficePage />
      </I18nProvider>
    </QueryClientProvider>
  );
  const result = render(ui());
  return { ...result, rerenderPage: () => result.rerender(ui()) };
}

async function openPanel(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Atlas/ }));
  return screen.getByRole("dialog");
}

describe("OfficePage", () => {
  beforeEach(() => {
    snapshot.tasks = [];
    activityState.byAgent = new Map();
    presenceState.byAgent = new Map([
      [
        "agent-1",
        { availability: "online", workload: "working", runningCount: 1, queuedCount: 0 },
      ],
    ]);
    dispatchState.items = [];
    apiState.listSquads.mockClear();
    room.peers = [];
    room.joined = true;
    room.error = null;
    room.sendMove.mockClear();
    push.mockClear();
    canvas.visitAgent.mockClear();
    canvas.playEmote.mockClear();
    canvas.deliverToReception.mockClear();
    openAgentChat.mockClear();
    quickCreate.mockReset();
    draftStore.setAgent.mockClear();
    draftStore.setActiveMode.mockClear();
    openModal.mockClear();
  });

  it("lists the signed-in member, peers and agents", () => {
    room.peers = [{ userId: "user-2", x: 10, y: 20, dir: null }];
    renderPage();

    expect(screen.getByRole("heading", { name: "Office" })).toBeInTheDocument();
    expect(screen.getByText("Alex Doe")).toBeInTheDocument();
    expect(screen.getByText("You")).toBeInTheDocument();
    expect(screen.getByText("Jamie Chen")).toBeInTheDocument();
    expect(screen.getByText("Atlas")).toBeInTheDocument();
    expect(screen.getByText("Working")).toBeInTheDocument();
    expect(screen.getByTestId("office-canvas")).toHaveTextContent("1");
  });

  it("captions a working agent with the issue it is running", () => {
    snapshot.tasks = [
      { id: "task-1", agent_id: "agent-1", issue_id: "issue-11", status: "running" },
    ];
    renderPage();

    expect(screen.getByTestId("activity-agent-1")).toHaveAttribute(
      "data-activity",
      "MAKE-11 Write the launch post",
    );
    expect(screen.getByTestId("company-name")).toHaveTextContent("eclicktech");
  });

  it("opens the interaction panel from the roster and walks over", async () => {
    const user = userEvent.setup();
    renderPage();

    const panel = await openPanel(user);

    expect(within(panel).getByText("Atlas")).toBeInTheDocument();
    expect(canvas.visitAgent).toHaveBeenCalledWith("agent-1");
    expect(push).not.toHaveBeenCalled();
  });

  it("opens the agent chat and detail from the panel", async () => {
    const user = userEvent.setup();
    renderPage();

    const panel = await openPanel(user);
    await user.click(within(panel).getByRole("button", { name: /Chat/ }));
    expect(openAgentChat).toHaveBeenCalledWith("agent-1");

    await user.click(within(panel).getByRole("button", { name: /Details/ }));
    expect(push).toHaveBeenCalledWith("/workspace-test/agents/agent-1");
  });

  it("hints at T when the player stands next to an agent", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "focus-agent" }));
    expect(screen.getByText("Press T to talk to Atlas")).toBeInTheDocument();

    await user.keyboard("t");
    expect(canvas.visitAgent).toHaveBeenCalledWith("agent-1");
  });

  it("assigns a quick task from the panel", async () => {
    quickCreate.mockResolvedValue({ task_id: "task-1" });
    const user = userEvent.setup();
    renderPage();

    const panel = await openPanel(user);
    await user.click(within(panel).getByRole("button", { name: /Assign task/ }));

    const input = within(panel).getByRole("textbox", { name: "Describe the task…" });
    await user.type(input, "Summarise the open bugs");
    await user.click(within(panel).getByRole("button", { name: /Send/ }));

    await waitFor(() =>
      expect(quickCreate).toHaveBeenCalledWith({
        agent_id: "agent-1",
        prompt: "Summarise the open bugs",
      }),
    );
    expect(canvas.playEmote).toHaveBeenCalledWith("agent-1", "check");
    await waitFor(() =>
      expect(
        within(panel).queryByRole("textbox", { name: "Describe the task…" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("reports a failed quick task inline", async () => {
    quickCreate.mockRejectedValue(new Error("offline"));
    const user = userEvent.setup();
    renderPage();

    const panel = await openPanel(user);
    await user.click(within(panel).getByRole("button", { name: /Assign task/ }));
    await user.type(
      within(panel).getByRole("textbox", { name: "Describe the task…" }),
      "Do the thing",
    );
    await user.click(within(panel).getByRole("button", { name: /Send/ }));

    expect(
      await within(panel).findByText("Couldn't create the task. Try again."),
    ).toBeInTheDocument();
    expect(canvas.playEmote).not.toHaveBeenCalled();
  });

  it("hands the brief to the full create dialog", async () => {
    const user = userEvent.setup();
    renderPage();

    const panel = await openPanel(user);
    await user.click(within(panel).getByRole("button", { name: /Assign task/ }));
    await user.type(
      within(panel).getByRole("textbox", { name: "Describe the task…" }),
      "Plan the release",
    );
    await user.click(within(panel).getByRole("button", { name: /More options/ }));

    expect(draftStore.setAgent).toHaveBeenCalledWith({
      prompt: "Plan the release",
      actorType: "agent",
      actorId: "agent-1",
    });
    expect(draftStore.setActiveMode).toHaveBeenCalledWith("agent");
    expect(openModal).toHaveBeenCalledWith("quick-create-issue", {
      agent_id: "agent-1",
    });
  });

  it("explains an empty room and reports the hint once connected", () => {
    renderPage({ members: [makeMember()], agents: [] });

    expect(screen.getByText("Nobody else is here yet.")).toBeInTheDocument();
    expect(screen.getByText("No agents in this workspace yet.")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Walk with WASD or the arrow keys, scroll to zoom, and walk up to an agent to press T.",
      ),
    ).toBeInTheDocument();
  });

  it("shows the connecting hint before the socket is ready", () => {
    room.joined = false;
    renderPage({ members: [makeMember()] });

    expect(screen.getByText("Connecting to the office…")).toBeInTheDocument();
  });

  it("levels agents, crowns the top contributor and levels the company", () => {
    activityState.byAgent = new Map([
      ["agent-1", activityWithCompleted(4)],
      ["agent-2", activityWithCompleted(9)],
    ]);
    renderPage({
      agents: [makeAgent(), makeAgent({ id: "agent-2", name: "Borealis" })],
    });

    expect(screen.getByText("Lv 2")).toBeInTheDocument();
    expect(screen.getByText("Lv 1")).toBeInTheDocument();
    expect(screen.getByText("Company Lv 1")).toBeInTheDocument();
    expect(screen.getByLabelText("Top contributor this month")).toBeInTheDocument();
  });

  it("unlocks office decorations once the company levels up", () => {
    activityState.byAgent = new Map([
      ["agent-1", activityWithCompleted(10)],
      ["agent-2", activityWithCompleted(9)],
    ]);
    renderPage({
      agents: [makeAgent(), makeAgent({ id: "agent-2", name: "Borealis" })],
    });

    expect(screen.getByText("Company Lv 2")).toBeInTheDocument();
    expect(screen.getByTestId("office-canvas")).toHaveAttribute(
      "data-company-level",
      "2",
    );
  });

  it("flags an agent stuck on an offline runtime", () => {
    presenceState.byAgent = new Map([
      [
        "agent-1",
        { availability: "offline", workload: "queued", runningCount: 0, queuedCount: 2 },
      ],
    ]);
    renderPage();

    expect(screen.getByTestId("activity-agent-1")).toHaveAttribute(
      "data-alert",
      "true",
    );
  });

  it("walks the squad leader to reception when a dispatch is delivered", () => {
    dispatchState.items = [makeDispatch()];
    const { rerenderPage } = renderPage({ squads: [SQUAD] });

    dispatchState.items = [makeDispatch({ status: "in_review" })];
    rerenderPage();

    expect(canvas.deliverToReception).toHaveBeenCalledWith("agent-1");
  });

  it("raises an alert when a dispatch fails", () => {
    dispatchState.items = [makeDispatch()];
    const { rerenderPage } = renderPage({ squads: [SQUAD] });

    dispatchState.items = [makeDispatch({ leaderState: "failed" })];
    rerenderPage();

    expect(canvas.playEmote).toHaveBeenCalledWith("agent-1", "alert");
  });

  it("does not replay delivered work when the page opens", () => {
    dispatchState.items = [makeDispatch({ status: "in_review" })];
    renderPage({ squads: [SQUAD] });

    expect(canvas.deliverToReception).not.toHaveBeenCalled();
  });
});
