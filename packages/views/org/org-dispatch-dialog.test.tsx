import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, type OrgAgent, type OrgSquad, type OrgTree, type OrgUnit } from "@multica/core/api";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { orgTreeOptions } from "@multica/core/workspace/queries";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { OrgDispatchDialog, orgDispatchTargets } from "./org-dispatch-dialog";

const WS = "ws-test";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { dispatchToOrgUnit: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => WS }));

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

function unit(overrides: Partial<OrgUnit> & Pick<OrgUnit, "id" | "name">): OrgUnit {
  return { ...baseUnit, ...overrides };
}

function tree(overrides: Partial<OrgTree> = {}): OrgTree {
  return {
    units: [
      unit({ id: "dept-root", name: "Engineering" }),
      unit({ id: "dept-pay", parent_id: "dept-root", name: "Payments", lead_user_id: "u-ada" }),
      unit({ id: "dept-empty", parent_id: "dept-root", name: "Research" }),
    ],
    agents: [{ ...baseAgent, id: "agent-rev", name: "Reviewer", unit_id: "dept-pay", owner_id: "u-ada" }],
    members: [],
    squads: [],
    can_manage: false,
    my_unit_id: "dept-pay",
    ...overrides,
  };
}

const navAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/org",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

let qc: QueryClient;

beforeEach(() => {
  vi.clearAllMocks();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
});

function renderDialog(data: OrgTree) {
  qc.setQueryData(orgTreeOptions(WS).queryKey, data);
  const onOpenChange = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={navAdapter}>
          <OrgDispatchDialog open onOpenChange={onOpenChange} />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

describe("orgDispatchTargets", () => {
  it("uses the department lead's twin, then the first subtree agent, as leader", () => {
    const targets = orgDispatchTargets(tree());
    const root = targets.find((target) => target.id === "dept-root");
    const pay = targets.find((target) => target.id === "dept-pay");
    const empty = targets.find((target) => target.id === "dept-empty");

    // The root has no lead of its own but its subtree contains Reviewer.
    expect(root).toMatchObject({ path: "Engineering", leader: "Reviewer", agentCount: 1, dispatchable: true });
    expect(pay).toMatchObject({ path: "Engineering/Payments", leader: "Reviewer", dispatchable: true });
    expect(empty).toMatchObject({ path: "Engineering/Research", leader: "", agentCount: 0, dispatchable: false });
  });

  it("counts an existing squad's members even when its leader sits outside the subtree", () => {
    const targets = orgDispatchTargets(tree({
      units: [unit({ id: "dept-only", name: "Design" })],
      agents: [{ ...baseAgent, id: "agent-lead", name: "Lead", unit_id: "elsewhere" }],
      squads: [{ ...baseSquad, id: "squad-1", unit_id: "dept-only", leader_id: "agent-lead", agent_count: 2 }],
    }));

    expect(targets[0]).toMatchObject({ leader: "Lead", agentCount: 2, dispatchable: true });
  });
});

describe("OrgDispatchDialog", () => {
  it("dispatches to the requester's department and opens the created issue", async () => {
    vi.mocked(api.dispatchToOrgUnit).mockResolvedValue({
      id: "issue-1",
    } as Awaited<ReturnType<typeof api.dispatchToOrgUnit>>);
    const { onOpenChange } = renderDialog(tree());

    expect(screen.getByText("Engineering/Payments · Led by Reviewer")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Rework checkout" } });
    fireEvent.change(screen.getByLabelText("Goal"), { target: { value: "Split the flow" } });
    fireEvent.change(screen.getByLabelText("Input"), { target: { value: "Figma link" } });
    fireEvent.change(screen.getByLabelText("Acceptance criteria"), { target: { value: "Tests pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(api.dispatchToOrgUnit).toHaveBeenCalledWith("dept-pay", {
        title: "Rework checkout",
        description: "## Goal\nSplit the flow\n\n## Input\nFigma link\n\n## Acceptance\nTests pass",
      }),
    );
    await waitFor(() => expect(navAdapter.push).toHaveBeenCalledWith("/acme/issues/issue-1"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps the brief unsendable without a goal and acceptance criteria", () => {
    renderDialog(tree());

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Rework checkout" } });
    const send = screen.getByRole("button", { name: "Send" });
    expect(send).toBeDisabled();
    expect(api.dispatchToOrgUnit).not.toHaveBeenCalled();
  });

  it("shows an empty state when the requester cannot dispatch anywhere", () => {
    renderDialog(tree({
      units: [unit({ id: "dept-empty", name: "Research" })],
      agents: [],
      my_unit_id: "dept-empty",
    }));

    expect(screen.getByText("No department can take work yet. Add an agent to a department first.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });
});
