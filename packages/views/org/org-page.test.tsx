import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { api, type OrgAgent, type OrgMember, type OrgSquad, type OrgTree, type OrgUnit } from "@multica/core/api";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    createOrgUnit: vi.fn(),
    updateOrgUnit: vi.fn(),
    deleteOrgUnit: vi.fn(),
    setOrgMemberUnit: vi.fn(),
    setOrgAgentUnit: vi.fn(),
    setOrgSquadUnit: vi.fn(),
    generateOrgUnitSquad: vi.fn(),
    registerIntakeAgent: vi.fn(),
    patchIntakeAgent: vi.fn(),
  },
}));
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { memberListOptions, orgSyncStatusOptions, orgTreeOptions, workspaceListOptions } from "@multica/core/workspace/queries";
import type { MemberWithUser } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { OrgPage } from "./org-page";

const WS = "ws-test";
const NOW = Date.parse("2026-11-12T08:00:00Z");

const member: MemberWithUser = {
  id: "m-1",
  workspace_id: WS,
  user_id: "u-ada",
  role: "member",
  created_at: new Date(NOW).toISOString(),
  name: "Ada",
  email: "ada@example.com",
  avatar_url: null,
};

const baseUnit: OrgUnit = {
  id: "",
  parent_id: "",
  name: "",
  lead_user_id: "",
  can_manage: false,
  description: "",
  position: 0,
};
const baseAgent: OrgAgent = { id: "", name: "", unit_id: "", owner_id: "", runtime_mode: "", can: "", cannot: "", status: "" };
const baseSquad: OrgSquad = {
  id: "",
  name: "",
  unit_id: "",
  leader_id: "",
  agent_count: 0,
  working_agents: 0,
  wip_limit: 3,
  max_children: 8,
  active_issues: 0,
  review_issues: 0,
  blocked_issues: 0,
};
const baseOrgMember: OrgMember = { user_id: "", unit_id: "", name: "", email: "", avatar_url: null };

function unit(overrides: Partial<OrgUnit> & Pick<OrgUnit, "id" | "name">): OrgUnit {
  return { ...baseUnit, ...overrides };
}

function tree(overrides: Partial<OrgTree> = {}): OrgTree {
  return {
    units: [
      unit({ id: "dept-root", name: "Engineering", can_manage: true }),
      unit({ id: "dept-child", parent_id: "dept-root", name: "Payments", can_manage: true }),
    ],
    agents: [{ ...baseAgent, id: "agent-1", name: "Reviewer", unit_id: "dept-child", status: "idle" }],
    members: [{ ...baseOrgMember, user_id: "u-ada", unit_id: "dept-child", name: "Ada", email: "ada@example.com" }],
    squads: [],
    can_manage: true,
    my_unit_id: "",
    ...overrides,
  };
}

let qc: QueryClient;
let auth: ReturnType<typeof createAuthStore>;

function signIn(userID: string) {
  auth.setState({
    user: {
      id: userID,
      name: "Ada",
      email: "ada@example.com",
      avatar_url: null,
      onboarded_at: null,
      onboarding_questionnaire: {},
      language: null,
      profile_description: "",
      timezone: null,
      is_operator: false,
      created_at: new Date(NOW).toISOString(),
      updated_at: new Date(NOW).toISOString(),
    },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  auth = createAuthStore({
    api,
    storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  });
  registerAuthStore(auth);
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(workspaceListOptions().queryKey, [{
    id: WS,
    name: "Acme",
    slug: "acme",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACM",
    avatar_url: null,
    created_at: new Date(NOW).toISOString(),
    updated_at: new Date(NOW).toISOString(),
  }]);
  qc.setQueryData(memberListOptions(WS).queryKey, [member]);
  qc.setQueryData(orgSyncStatusOptions(WS).queryKey, { configured: true, last_run: null });
});

afterEach(() => {
  qc.clear();
  vi.restoreAllMocks();
});

const navAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/org",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function renderPage(data: OrgTree) {
  qc.setQueryData(orgTreeOptions(WS).queryKey, data);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={navAdapter}>
          <OrgPage />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

function selectUnit(name: string) {
  fireEvent.click(screen.getAllByRole("button", { name: new RegExp(`^${name}`) })[0]!);
}

describe("OrgPage permissions", () => {
  it("keeps a plain member read-only", () => {
    renderPage(tree({
      units: [
        unit({ id: "dept-root", name: "Engineering" }),
        unit({ id: "dept-child", parent_id: "dept-root", name: "Payments" }),
      ],
      can_manage: false,
    }));

    expect(screen.getByText(/You only see your own department/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add top-level department" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sync from Feishu" })).not.toBeInTheDocument();

    selectUnit("Payments");

    expect(screen.getByText("Ada")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("lets a unit lead place unassigned people", () => {
    renderPage(tree({
      members: [{ ...baseOrgMember, user_id: "u-grace", name: "Grace", email: "grace@example.com" }],
    }));

    fireEvent.click(screen.getByRole("button", { name: /^Unassigned/ }));

    expect(screen.getByText("Grace")).toBeInTheDocument();
    expect(screen.getAllByRole("combobox")).toHaveLength(1);
  });

  it("keeps unassigned people read-only without unit rights", () => {
    renderPage(tree({
      units: [unit({ id: "dept-root", name: "Engineering" })],
      members: [{ ...baseOrgMember, user_id: "u-grace", name: "Grace", email: "grace@example.com" }],
      can_manage: false,
    }));

    fireEvent.click(screen.getByRole("button", { name: /^Unassigned/ }));

    expect(screen.getByText("Grace")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("offers lead and move controls only inside a manageable subtree", () => {
    renderPage(tree({
      units: [
        unit({ id: "dept-root", name: "Engineering" }),
        unit({ id: "dept-child", parent_id: "dept-root", name: "Payments", can_manage: true }),
      ],
      can_manage: false,
    }));

    selectUnit("Engineering");
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();

    selectUnit("Payments");
    // lead picker, the member's department picker, and the agent's picker.
    expect(screen.getAllByRole("combobox")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Rename" })).toBeInTheDocument();
  });
});

describe("OrgPage Feishu sync", () => {
  it("keeps the sync button disabled until credentials are configured", () => {
    qc.setQueryData(orgSyncStatusOptions(WS).queryKey, { configured: false, last_run: null });
    renderPage(tree());

    expect(screen.getByRole("button", { name: "Sync from Feishu" })).toBeDisabled();
    expect(screen.getByText(/MULTICA_FEISHU_APP_ID/)).toBeInTheDocument();
  });

  it("shows the last run report", () => {
    qc.setQueryData(orgSyncStatusOptions(WS).queryKey, {
      configured: true,
      last_run: {
        created_units: [],
        updated_units: [],
        leads_assigned: [],
        members_added: ["Ada"],
        members_moved: [],
        members_without_email: [],
        detached_departments: [],
        detached_users: [],
        synced_at: "2026-11-12T08:00:00Z",
      },
    });
    renderPage(tree());

    expect(screen.getByText("People added: Ada")).toBeInTheDocument();
    expect(screen.getByText(/Last synced/)).toBeInTheDocument();
  });

  it("summarises long change lists instead of printing every name", () => {
    const people = Array.from({ length: 11 }, (_, index) => `Person ${index + 1}`);
    qc.setQueryData(orgSyncStatusOptions(WS).queryKey, {
      configured: true,
      last_run: {
        created_units: [],
        updated_units: [],
        leads_assigned: [],
        members_added: people,
        members_moved: [],
        members_without_email: [],
        detached_departments: [],
        detached_users: [],
        synced_at: "2026-11-12T08:00:00Z",
      },
    });
    renderPage(tree());

    expect(
      screen.getByText("People added: Person 1, Person 2, Person 3, Person 4, Person 5, Person 6, Person 7, Person 8 +3"),
    ).toBeInTheDocument();
  });
});

describe("OrgPage admin actions", () => {
  it("creates a top-level department from the sidebar", async () => {
    const create = vi.spyOn(api, "createOrgUnit").mockResolvedValue({ id: "dept-new" });
    renderPage(tree());

    fireEvent.change(screen.getByPlaceholderText("Department name"), { target: { value: "Support" } });
    fireEvent.click(screen.getByRole("button", { name: "Add top-level department" }));

    await waitFor(() => expect(create).toHaveBeenCalledWith({ name: "Support" }));
  });

  it("shows members and agents under the selected department", () => {
    renderPage(tree());

    selectUnit("Payments");

    expect(screen.getByText("Ada")).toBeInTheDocument();
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getAllByText("Engineering/Payments").length).toBeGreaterThan(0);
  });

  it("syncs the department squad from its header card", async () => {
    const generate = vi.spyOn(api, "generateOrgUnitSquad").mockResolvedValue({
      squad: baseSquad,
      added: 0,
      updated: 0,
      removed: 0,
      created: false,
    });
    renderPage(tree({
      units: [unit({ id: "dept-root", name: "Engineering", can_manage: true })],
      agents: [{ ...baseAgent, id: "agent-1", name: "Reviewer", unit_id: "dept-root", status: "idle" }],
      squads: [{
        ...baseSquad,
        id: "squad-1",
        name: "Engineering squad",
        unit_id: "dept-root",
        leader_id: "agent-1",
        agent_count: 1,
      }],
    }));

    selectUnit("Engineering");
    fireEvent.click(screen.getByRole("button", { name: "Sync squad" }));

    const dialog = screen.getByTestId("generate-squad-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Sync squad" }));

    await waitFor(() =>
      expect(generate).toHaveBeenCalledWith("dept-root", {
        name: "Engineering squad",
        leader_id: "agent-1",
        wip_limit: 3,
        max_children: 8,
        instructions: undefined,
      }),
    );
  });

  it("cannot generate a squad before the department has an agent", () => {
    renderPage(tree({ agents: [], squads: [] }));

    selectUnit("Engineering");

    expect(screen.getByRole("button", { name: "Generate squad" })).toBeDisabled();
  });
});

describe("OrgPage member landing and squads", () => {
  it("lands a plain member on their own department and shows what is waiting", () => {
    renderPage(tree({
      units: [
        unit({ id: "dept-root", name: "Engineering" }),
        unit({ id: "dept-child", parent_id: "dept-root", name: "Payments" }),
      ],
      agents: [{ ...baseAgent, id: "agent-1", name: "Reviewer", unit_id: "dept-child", status: "idle" }],
      squads: [{
        ...baseSquad,
        id: "squad-1",
        name: "Payments squad",
        unit_id: "dept-child",
        agent_count: 2,
        working_agents: 0,
      }],
      can_manage: false,
      my_unit_id: "dept-child",
    }));

    expect(screen.getByText("My department")).toBeInTheDocument();
    // The department detail is preselected without a click, and the squad
    // attached to it reports that it is waiting for scheduling.
    expect(screen.getAllByText("Payments squad").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Waiting for scheduling").length).toBeGreaterThan(0);
  });

  it("shows a member without a department that they are not placed yet", () => {
    renderPage(tree({
      units: [
        unit({ id: "dept-root", name: "Engineering" }),
        unit({ id: "dept-child", parent_id: "dept-root", name: "Payments" }),
      ],
      can_manage: false,
      my_unit_id: "",
    }));

    expect(screen.getByText(/You are not in a department yet/)).toBeInTheDocument();
    expect(screen.getByText(/Pick a department/)).toBeInTheDocument();
  });

  it("tells busy squads apart from waiting ones inside a department", () => {
    renderPage(tree({
      units: [unit({ id: "dept-root", name: "Engineering" })],
      agents: [],
      members: [],
      squads: [
        { ...baseSquad, id: "squad-waiting", name: "Waiting squad", unit_id: "dept-root", agent_count: 3, working_agents: 0 },
        { ...baseSquad, id: "squad-busy", name: "Busy squad", unit_id: "dept-root", agent_count: 2, working_agents: 1 },
        { ...baseSquad, id: "squad-empty", name: "Empty squad", unit_id: "dept-root", agent_count: 0, working_agents: 0 },
      ],
      my_unit_id: "dept-root",
    }));

    expect(screen.getByText("Waiting squad")).toBeInTheDocument();
    expect(screen.getByText("Busy squad")).toBeInTheDocument();
    expect(screen.getAllByText("Waiting for scheduling")).toHaveLength(1);
    expect(screen.getByText("Working together")).toBeInTheDocument();
    expect(screen.getByText("No agents assigned")).toBeInTheDocument();
  });

  it("offers placement for squads without a department", () => {
    renderPage(tree({
      squads: [{ ...baseSquad, id: "squad-1", name: "Loose squad", unit_id: "", agent_count: 1 }],
    }));

    fireEvent.click(screen.getByRole("button", { name: /^Unassigned/ }));

    expect(screen.getByText("Loose squad")).toBeInTheDocument();
    // The squad's own department picker; members and agents are already placed.
    expect(screen.getAllByRole("combobox")).toHaveLength(1);
  });
});

describe("OrgPage member twins", () => {
  const twinAgent: OrgAgent = {
    ...baseAgent,
    id: "agent-twin",
    name: "Ada twin",
    unit_id: "dept-child",
    owner_id: "u-ada",
    can: "Backend APIs",
    status: "idle",
  };

  it("shows a member's twin with its responsibility and availability", () => {
    renderPage(
      tree({
        units: [unit({ id: "dept-child", name: "Payments" })],
        agents: [twinAgent],
        my_unit_id: "dept-child",
        can_manage: false,
      }),
    );

    // The twin renders on the member row only, not a second time as a
    // shared agent card.
    expect(screen.getAllByText("Ada twin")).toHaveLength(1);
    expect(screen.getByText("Backend APIs")).toBeInTheDocument();
    expect(screen.getByText("Waiting for scheduling")).toBeInTheDocument();
    // Another member cannot edit someone else's twin.
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("lets the signed-in member register a twin into their department", async () => {
    signIn("u-ada");
    vi.mocked(api.registerIntakeAgent).mockResolvedValue({
      id: "agent-new",
      name: "Ada twin",
      can: "Backend APIs",
    });
    renderPage(
      tree({
        units: [unit({ id: "dept-child", name: "Payments" })],
        agents: [],
        my_unit_id: "dept-child",
        can_manage: false,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Register my digital twin" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Ada twin" } });
    fireEvent.change(screen.getByLabelText("Does"), { target: { value: "Backend APIs" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(api.registerIntakeAgent).toHaveBeenCalledWith({
        name: "Ada twin",
        can: "Backend APIs",
        cannot: "",
        runtime_mode: "local",
      }),
    );
    await waitFor(() => expect(api.setOrgAgentUnit).toHaveBeenCalledWith("agent-new", "dept-child"));
  });

  it("lets a department lead register a twin for a direct report", async () => {
    signIn("u-lead");
    vi.mocked(api.registerIntakeAgent).mockResolvedValue({
      id: "agent-new",
      name: "Ada twin",
      can: "Backend APIs",
    });
    renderPage(
      tree({
        units: [unit({ id: "dept-child", name: "Payments", can_manage: true })],
        agents: [],
        my_unit_id: "dept-child",
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Register Ada's twin" }));
    const dialog = screen.getByTestId("twin-dialog");
    expect(within(dialog).getByText("Ada's digital twin")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Ada twin" } });
    fireEvent.change(within(dialog).getByLabelText("Does"), { target: { value: "Backend APIs" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(api.registerIntakeAgent).toHaveBeenCalledWith({
        name: "Ada twin",
        can: "Backend APIs",
        cannot: "",
        runtime_mode: "local",
        owner_id: "u-ada",
      }),
    );
    await waitFor(() => expect(api.setOrgAgentUnit).toHaveBeenCalledWith("agent-new", "dept-child"));
  });

  it("keeps a report's twin read-only for non-leads", () => {
    signIn("u-someone-else");
    renderPage(
      tree({
        units: [unit({ id: "dept-child", name: "Payments", can_manage: false })],
        agents: [twinAgent],
        my_unit_id: "dept-child",
      }),
    );

    expect(screen.getAllByText("Ada twin")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Register Ada's twin" })).not.toBeInTheDocument();
  });

  it("edits the signed-in member's twin without moving it", async () => {
    signIn("u-ada");
    renderPage(
      tree({
        units: [unit({ id: "dept-child", name: "Payments" })],
        agents: [twinAgent],
        my_unit_id: "dept-child",
        can_manage: false,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const nameInput = screen.getByLabelText("Name") as HTMLInputElement;
    expect(nameInput.value).toBe("Ada twin");
    fireEvent.change(nameInput, { target: { value: "Ada digital twin" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(api.patchIntakeAgent).toHaveBeenCalledWith("agent-twin", {
        name: "Ada digital twin",
        can: "Backend APIs",
        cannot: "",
        runtime_mode: "local",
      }),
    );
    expect(api.setOrgAgentUnit).not.toHaveBeenCalled();
  });

  it("creates a sub-department from the department card dialog", async () => {
    vi.mocked(api.createOrgUnit).mockResolvedValue({ id: "dept-new" });
    renderPage(tree({ my_unit_id: "dept-root" }));

    fireEvent.click(screen.getAllByRole("button", { name: "Add sub-department" })[0]!);
    const dialog = screen.getByTestId("create-unit-dialog");
    fireEvent.change(within(dialog).getByPlaceholderText("Department name"), {
      target: { value: "Payments two" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "New department" }));

    await waitFor(() =>
      expect(api.createOrgUnit).toHaveBeenCalledWith({ name: "Payments two", parent_id: "dept-root" }),
    );
  });
});
