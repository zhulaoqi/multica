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
  orgTreeOptions,
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
  celebrate: vi.fn(),
  archiveRitual: vi.fn(),
  setMinimapVisible: vi.fn(),
}));
const review = vi.hoisted(() => ({
  updateIssue: vi.fn(),
  createComment: vi.fn(),
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
  listOrgUnits: vi.fn().mockResolvedValue({
    units: [],
    agents: [],
    members: [],
    squads: [],
    can_manage: false,
    my_unit_id: "",
  }),
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
  api: {
    quickCreateIssue: quickCreate,
    listSquads: apiState.listSquads,
    listOrgUnits: apiState.listOrgUnits,
  },
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
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: review.updateIssue, isPending: false }),
  useCreateComment: () => ({ mutateAsync: review.createComment, isPending: false }),
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
      season,
      deskTraces,
      roomBadges,
      deskZones,
      minimapVisible,
      receptionPapers,
      onFocusAgentChange,
      onPlaceFocusChange,
      onRandomEvent,
    }: {
      agents: {
        id: string;
        home?: number;
        activity?: string | null;
        alert?: boolean;
        ownerName?: string | null;
        unitPath?: string | null;
      }[];
      peers: unknown[];
      companyName: string;
      companyLevel: number;
      season: { id: string };
      deskTraces?: unknown[];
      roomBadges?: unknown[];
      deskZones?: {
        label: string;
        pathLabel?: string;
        children?: string[];
        level: number;
      }[];
      minimapVisible?: boolean;
      receptionPapers: number;
      onFocusAgentChange: (agentId: string | null) => void;
      onPlaceFocusChange?: (place: string | null) => void;
      onRandomEvent?: (event: string) => void;
    },
    ref: Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => canvas);
    return (
      <div data-testid="office-canvas" data-company-level={companyLevel}>
        {peers.length}
        <span data-testid="company-name">{companyName}</span>
        <span data-testid="reception-papers">{receptionPapers}</span>
        <span data-testid="office-season">{season.id}</span>
        <span data-testid="desk-traces">{(deskTraces ?? []).length}</span>
        <span data-testid="room-badges">{(roomBadges ?? []).length}</span>
        <span data-testid="desk-zones">
          {(deskZones ?? [])
            .map((zone) => zone.pathLabel ?? zone.label)
            .join(",")}
        </span>
        <span data-testid="desk-zone-children">
          {(deskZones ?? [])
            .flatMap((zone) => zone.children ?? [])
            .join(",")}
        </span>
        <span data-testid="minimap-visible">{String(minimapVisible)}</span>
        {agents.map((agent) => (
          <span
            key={agent.id}
            data-testid={`activity-${agent.id}`}
            data-home={agent.home ?? ""}
            data-activity={agent.activity ?? ""}
            data-alert={agent.alert ? "true" : ""}
            data-owner={agent.ownerName ?? ""}
            data-unit={agent.unitPath ?? ""}
          />
        ))}
        <button type="button" onClick={() => onFocusAgentChange("agent-1")}>
          focus-agent
        </button>
        <button
          type="button"
          onClick={() => {
            onFocusAgentChange(null);
            onPlaceFocusChange?.("reception");
          }}
        >
          at-reception
        </button>
        <button
          type="button"
          onClick={() => {
            onFocusAgentChange(null);
            onPlaceFocusChange?.("coffee");
          }}
        >
          at-coffee
        </button>
        <button
          type="button"
          onClick={() => {
            onFocusAgentChange(null);
            onPlaceFocusChange?.("meeting");
          }}
        >
          at-meeting
        </button>
        <button type="button" onClick={() => onRandomEvent?.("cat")}>
          random-event
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

function makeOrgTree({
  units = [
    { id: "unit-aihub", name: "AIHUB", position: 0, can_manage: true },
  ],
  members = [
    { user_id: "user-1", unit_id: "unit-aihub" },
    { user_id: "user-2", unit_id: "unit-aihub" },
  ],
  agents = [
    { id: "agent-1", unit_id: "unit-aihub" },
    { id: "agent-2", unit_id: "unit-aihub" },
  ],
}: {
  units?: {
    id: string;
    name: string;
    parent_id?: string;
    position?: number;
    can_manage: boolean;
  }[];
  members?: { user_id: string; unit_id: string }[];
  agents?: { id: string; unit_id: string }[];
} = {}) {
  return {
    units: units.map((unit) => ({
      parent_id: "",
      lead_user_id: "",
      description: "",
      intake_sources: [],
      ...unit,
    })),
    agents,
    members,
    squads: [],
    can_manage: true,
    my_unit_id: "unit-aihub",
  };
}

function renderPage({
  members = [makeMember(), makeMember({ id: "member-2", user_id: "user-2", name: "Jamie Chen" })],
  agents = [makeAgent()],
  squads = [],
  orgTree,
  variant = "workspace",
}: {
  members?: MemberWithUser[];
  agents?: Agent[];
  squads?: unknown[];
  orgTree?: unknown;
  variant?: "workspace" | "home";
} = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(memberListOptions(WS).queryKey, members);
  qc.setQueryData(agentListOptions(WS).queryKey, agents);
  if (orgTree) qc.setQueryData(orgTreeOptions(WS).queryKey, orgTree as never);
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
        <OfficePage variant={variant} />
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
    canvas.celebrate.mockClear();
    canvas.archiveRitual.mockClear();
    canvas.setMinimapVisible.mockClear();
    apiState.listOrgUnits.mockClear();
    review.updateIssue.mockReset().mockResolvedValue({});
    review.createComment.mockReset().mockResolvedValue({});
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

  it("shows only the agents on the caller's own work on the C-end", () => {
    dispatchState.items = [makeDispatch()];
    renderPage({
      variant: "home",
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
      ],
      squads: [SQUAD],
    });

    // Company-wide chrome is gone: no page title, roster or trophy wall.
    expect(screen.queryByRole("heading", { name: "Office" })).not.toBeInTheDocument();
    expect(screen.queryByText("Alex Doe")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Trophy wall" }),
    ).not.toBeInTheDocument();

    // Only the squad leader on the caller's dispatch is in the room.
    expect(screen.getByTestId("activity-agent-1")).toBeInTheDocument();
    expect(screen.queryByTestId("activity-agent-2")).not.toBeInTheDocument();
  });

  it("shows a department lead their whole team, seated by department", () => {
    renderPage({
      variant: "home",
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
        makeAgent({ id: "agent-3", name: "Orion", owner_id: "user-3" }),
      ],
      orgTree: makeOrgTree({
        agents: [
          { id: "agent-1", unit_id: "unit-aihub" },
          { id: "agent-2", unit_id: "unit-aihub" },
          { id: "agent-3", unit_id: "unit-aihub" },
        ],
      }),
    });

    // Team members' agents show up even though the lead sent no dispatch.
    expect(screen.getByTestId("activity-agent-1")).toBeInTheDocument();
    expect(screen.getByTestId("activity-agent-2")).toBeInTheDocument();
    expect(screen.queryByTestId("activity-agent-3")).not.toBeInTheDocument();

    // AIHUB owns the west cluster and gets its plaque.
    expect(screen.getByTestId("desk-zones")).toHaveTextContent("AIHUB");
    expect(screen.getByTestId("activity-agent-1")).toHaveAttribute("data-home", "0");
    expect(screen.getByTestId("activity-agent-2")).toHaveAttribute("data-home", "1");
  });

  it("shows who each agent belongs to and its full department path", () => {
    renderPage({
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
      ],
      orgTree: makeOrgTree({
        units: [
          { id: "unit-rd", name: "研发部", position: 0, can_manage: true },
          {
            id: "unit-aihub",
            name: "AIHUB",
            parent_id: "unit-rd",
            position: 0,
            can_manage: false,
          },
        ],
        agents: [
          { id: "agent-1", unit_id: "unit-aihub" },
          { id: "agent-2", unit_id: "unit-aihub" },
        ],
      }),
    });

    expect(screen.getByTestId("activity-agent-1")).toHaveAttribute(
      "data-owner",
      "Alex Doe",
    );
    expect(screen.getByTestId("activity-agent-1")).toHaveAttribute(
      "data-unit",
      "研发部/AIHUB",
    );
    expect(screen.getByTestId("activity-agent-2")).toHaveAttribute(
      "data-owner",
      "Jamie Chen",
    );
    expect(screen.getByTestId("desk-zones")).toHaveTextContent("研发部 › AIHUB");
  });

  it("keeps a plain member on their own dispatches despite the org chart", () => {
    dispatchState.items = [makeDispatch()];
    renderPage({
      variant: "home",
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
      ],
      squads: [SQUAD],
      orgTree: makeOrgTree({
        units: [
          { id: "unit-aihub", name: "AIHUB", position: 0, can_manage: false },
        ],
      }),
    });

    expect(screen.getByTestId("activity-agent-1")).toBeInTheDocument();
    expect(screen.queryByTestId("activity-agent-2")).not.toBeInTheDocument();
  });

  it("shows workspace admins every agent on the C-end", () => {
    renderPage({
      variant: "home",
      members: [makeMember({ role: "owner" })],
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
        makeAgent({ id: "agent-3", name: "Orion", owner_id: "user-3" }),
      ],
    });

    expect(screen.getByTestId("activity-agent-1")).toBeInTheDocument();
    expect(screen.getByTestId("activity-agent-2")).toBeInTheDocument();
    expect(screen.getByTestId("activity-agent-3")).toBeInTheDocument();
  });

  it("zones desks and keeps room plaques in the workspace office", () => {
    renderPage({
      variant: "workspace",
      agents: [
        makeAgent(),
        makeAgent({ id: "agent-2", name: "Nova", owner_id: "user-2" }),
      ],
      orgTree: makeOrgTree(),
    });

    expect(screen.getByTestId("desk-zones")).toHaveTextContent("AIHUB");
    expect(screen.getByTestId("room-badges")).toHaveTextContent("1");
  });

  it("explains an empty C-end office instead of showing a blank room", () => {
    renderPage({ variant: "home" });
    expect(
      screen.getByText("Agents working on your requests will show up here."),
    ).toBeInTheDocument();
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

  it("summarises today's work for the C-end", () => {
    activityState.byAgent = new Map([["agent-1", activityWithCompleted(4)]]);
    renderPage({ variant: "home", members: [makeMember({ role: "owner" })] });

    const brief = screen.getByTestId("office-brief");
    expect(brief).toHaveTextContent("Done today 4");
    expect(brief).toHaveTextContent("Running 1");
    expect(brief).toHaveTextContent("Busiest Atlas");
  });

  it("hides the C-end brief when nothing has happened today", () => {
    presenceState.byAgent = new Map();
    renderPage({ variant: "home" });

    expect(screen.queryByTestId("office-brief")).not.toBeInTheDocument();
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

  it("checks in at the front desk for today's report", async () => {
    activityState.byAgent = new Map([["agent-1", activityWithCompleted(4)]]);
    dispatchState.items = [makeDispatch({ status: "in_review" })];
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "at-reception" }));
    expect(
      screen.getByText("Press T to check in at the front desk"),
    ).toBeInTheDocument();

    await user.keyboard("t");
    const panel = screen.getByRole("dialog");
    expect(within(panel).getByText("Front desk")).toBeInTheDocument();
    expect(within(panel).getByText("4 tasks completed")).toBeInTheDocument();
    expect(within(panel).getByText("4 / 6")).toBeInTheDocument();
    expect(within(panel).getByText("Ship the release notes")).toBeInTheDocument();
    expect(screen.getByTestId("reception-papers")).toHaveTextContent("1");
  });

  it("signs off a delivered result from the front desk", async () => {
    dispatchState.items = [makeDispatch({ status: "in_review" })];
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "at-reception" }));
    await user.keyboard("t");
    const panel = screen.getByRole("dialog");
    await user.click(within(panel).getByRole("button", { name: /Accept/ }));

    await waitFor(() =>
      expect(review.updateIssue).toHaveBeenCalledWith({
        id: "issue-root",
        status: "done",
      }),
    );
    expect(canvas.archiveRitual).toHaveBeenCalled();
  });

  it("celebrates a company level-up as the activity moves", async () => {
    activityState.byAgent = new Map([["agent-1", activityWithCompleted(4)]]);
    const { rerenderPage } = renderPage();
    expect(screen.queryByText("Company Lv 2 unlocked!")).not.toBeInTheDocument();

    activityState.byAgent = new Map([["agent-1", activityWithCompleted(20)]]);
    rerenderPage();

    expect(await screen.findByText("Company Lv 2 unlocked!")).toBeInTheDocument();
    expect(canvas.celebrate).toHaveBeenCalled();
  });

  it("celebrates the shared weekly goal when the team reaches it", async () => {
    activityState.byAgent = new Map([["agent-1", activityWithCompleted(2)]]);
    const { rerenderPage } = renderPage();
    expect(screen.queryByText("Weekly goal reached")).not.toBeInTheDocument();

    activityState.byAgent = new Map([["agent-1", activityWithCompleted(7)]]);
    rerenderPage();

    expect(await screen.findByText("Weekly goal reached")).toBeInTheDocument();
    expect(canvas.celebrate).toHaveBeenCalled();
  });

  it("runs the archive ritual when a delivered dispatch completes", () => {
    dispatchState.items = [makeDispatch()];
    const { rerenderPage } = renderPage({ squads: [SQUAD] });

    dispatchState.items = [makeDispatch({ status: "in_review" })];
    rerenderPage();
    dispatchState.items = [
      { ...makeDispatch({ status: "done" }), completed: true },
    ];
    rerenderPage();

    expect(canvas.archiveRitual).toHaveBeenCalled();
  });

  it("passes the season and today's desk traces to the scene", () => {
    activityState.byAgent = new Map([["agent-1", activityWithCompleted(4)]]);
    renderPage();

    expect(screen.getByTestId("office-season")).not.toBeEmptyDOMElement();
    expect(screen.getByTestId("desk-traces")).toHaveTextContent("1");
  });

  it("opens the kitchen chat and the meeting board on the floor", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "at-coffee" }));
    expect(
      screen.getByText("Press T for a kitchen chat"),
    ).toBeInTheDocument();
    await user.keyboard("t");
    expect(screen.getByText("Kitchen chat")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close" }));

    await user.click(screen.getByRole("button", { name: "at-meeting" }));
    expect(
      screen.getByText("Press T to read the meeting board"),
    ).toBeInTheDocument();
    await user.keyboard("t");
    expect(screen.getByText("Meeting room")).toBeInTheDocument();
  });

  it("opens the trophy wall and shows the current season", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "Trophy wall" }));

    expect(screen.getByText("Trophy wall")).toBeInTheDocument();
    expect(screen.getByText("First delivery")).toBeInTheDocument();
    expect(screen.getByText("Season")).toBeInTheDocument();
  });

  it("hides the minimap from the toolbar", async () => {
    const user = userEvent.setup();
    renderPage();
    expect(screen.getByTestId("minimap-visible")).toHaveTextContent("true");

    await user.click(screen.getByRole("button", { name: "Hide minimap" }));
    expect(screen.getByTestId("minimap-visible")).toHaveTextContent("false");
  });

  it("shows a toast when a random office event fires", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: "random-event" }));

    expect(
      screen.getByText("The office cat padded through."),
    ).toBeInTheDocument();
  });
});
