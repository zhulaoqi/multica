import type { ReactNode, Ref } from "react";
import { forwardRef, useImperativeHandle } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
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
vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({
    byAgent: new Map([
      [
        "agent-1",
        { availability: "online", workload: "working", runningCount: 1, queuedCount: 0 },
      ],
    ]),
    loading: false,
  }),
}));
vi.mock("@multica/core/office", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/office")>();
  return { ...actual, useOfficeRoom: () => room };
});
vi.mock("@multica/core/api", () => ({
  api: { quickCreateIssue: quickCreate },
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
      peers,
      onFocusAgentChange,
    }: {
      peers: unknown[];
      onFocusAgentChange: (agentId: string | null) => void;
    },
    ref: Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => canvas);
    return (
      <div data-testid="office-canvas">
        {peers.length}
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

function renderPage({
  members = [makeMember(), makeMember({ id: "member-2", user_id: "user-2", name: "Jamie Chen" })],
  agents = [makeAgent()],
}: {
  members?: MemberWithUser[];
  agents?: Agent[];
} = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(memberListOptions(WS).queryKey, members);
  qc.setQueryData(agentListOptions(WS).queryKey, agents);
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <OfficePage />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function openPanel(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Atlas/ }));
  return screen.getByRole("dialog");
}

describe("OfficePage", () => {
  beforeEach(() => {
    room.peers = [];
    room.joined = true;
    room.error = null;
    room.sendMove.mockClear();
    push.mockClear();
    canvas.visitAgent.mockClear();
    canvas.playEmote.mockClear();
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
      screen.getByText("Move with WASD or the arrow keys, or click the floor to walk."),
    ).toBeInTheDocument();
  });

  it("shows the connecting hint before the socket is ready", () => {
    room.joined = false;
    renderPage({ members: [makeMember()] });

    expect(screen.getByText("Connecting to the office…")).toBeInTheDocument();
  });
});
