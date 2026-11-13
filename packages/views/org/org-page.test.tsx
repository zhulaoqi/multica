import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, type OrgAgent, type OrgMember, type OrgTree, type OrgUnit } from "@multica/core/api";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    createOrgUnit: vi.fn(),
    updateOrgUnit: vi.fn(),
    deleteOrgUnit: vi.fn(),
    setOrgMemberUnit: vi.fn(),
    setOrgAgentUnit: vi.fn(),
  },
}));
import { WorkspaceSlugProvider } from "@multica/core/paths";
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
const baseAgent: OrgAgent = { id: "", name: "", unit_id: "", can: "", cannot: "" };
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
    agents: [{ ...baseAgent, id: "agent-1", name: "Reviewer", unit_id: "dept-child" }],
    members: [{ ...baseOrgMember, user_id: "u-ada", unit_id: "dept-child", name: "Ada", email: "ada@example.com" }],
    can_manage: true,
    ...overrides,
  };
}

let qc: QueryClient;

beforeEach(() => {
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

function renderPage(data: OrgTree) {
  qc.setQueryData(orgTreeOptions(WS).queryKey, data);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <OrgPage />
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

    expect(screen.getByText(/Only workspace admins/)).toBeInTheDocument();
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
});
