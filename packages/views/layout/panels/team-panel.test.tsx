import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { api, setApiInstance, type ApiClient } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
  orgTreeOptions,
  skillListOptions,
  squadListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { TeamPanel } from "./team-panel";

const WS = "ws-test";
const NOW = Date.parse("2026-11-04T08:00:00Z");
const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/runtimes",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-online",
    workspace_id: WS,
    daemon_id: "daemon-1",
    name: "Claude (dev.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: new Date(NOW - 42 * 60_000).toISOString(),
    created_at: new Date(NOW - 60 * 60_000).toISOString(),
    updated_at: new Date(NOW - 60 * 60_000).toISOString(),
    ...overrides,
  };
}

let qc: QueryClient;
let auth: ReturnType<typeof createAuthStore>;

beforeEach(() => {
  vi.spyOn(Date, "now").mockReturnValue(NOW);
  setApiInstance({ getBaseUrl: () => "" } as unknown as ApiClient);
  auth = createAuthStore({
    api,
    storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  });
  registerAuthStore(auth);
  qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
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
  for (const options of [
    agentListOptions(WS),
    agentTaskSnapshotOptions(WS),
    memberListOptions(WS),
    skillListOptions(WS),
    squadListOptions(WS),
  ]) {
    qc.setQueryData(options.queryKey, []);
  }
});

afterEach(() => {
  qc.clear();
  vi.restoreAllMocks();
});

function renderPanel(runtimes: AgentRuntime[]) {
  qc.setQueryData(runtimeListOptions(WS).queryKey, runtimes);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <TeamPanel />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("TeamPanel runtime badge", () => {
  it("shows a green online machine count without a total or duplicate providers", () => {
    renderPanel([
      ...Array.from({ length: 4 }, (_, index) => [
        runtime({ id: `online-${index}`, daemon_id: `daemon-${index}` }),
        runtime({ id: `online-codex-${index}`, daemon_id: `daemon-${index}`, provider: "codex" }),
      ]).flat(),
      ...Array.from({ length: 4 }, (_, index) =>
        runtime({
          id: `stale-${index}`,
          daemon_id: `daemon-${index}`,
          provider: "copilot",
          status: "offline",
        }),
      ),
      runtime({ id: "offline-claude", daemon_id: "daemon-offline", status: "offline" }),
      runtime({
        id: "offline-codex",
        daemon_id: "daemon-offline",
        provider: "codex",
        status: "offline",
      }),
    ]);

    const link = screen.getByRole("link", { name: /^Runtimes/ });
    expect(within(link).getByText("4 online")).toHaveClass("text-success");
    expect(link).not.toHaveTextContent(/offline|\//);
  });

  it("updates the count and warning when the last online provider disconnects and reconnects", async () => {
    const online = runtime();
    const stale = runtime({ id: "rt-stale", provider: "codex", status: "offline" });
    renderPanel([online, stale]);
    const link = screen.getByRole("link", { name: /^Runtimes/ });

    expect(within(link).getByText("1 online")).toHaveClass("text-success");

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [{ ...online, status: "offline" }, stale]);
    });
    await waitFor(() => {
      expect(within(link).getByText("0 online")).toHaveClass("text-warning");
    });

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [online, stale]);
    });
    await waitFor(() => {
      expect(within(link).getByText("1 online")).toHaveClass("text-success");
    });
  });

  it("does not count Qoder cloud services as machines", () => {
    renderPanel([
      runtime(),
      runtime({
        id: "rt-qoder-cloud",
        daemon_id: "qoder-daemon",
        provider: "qoder_cloud",
        runtime_mode: "cloud",
      }),
    ]);

    const link = screen.getByRole("link", { name: /^Runtimes/ });
    expect(within(link).getByText("1 online")).toHaveClass("text-success");
  });

  it("hides the badge when there are no machines", () => {
    renderPanel([]);

    expect(screen.getByRole("link", { name: "Runtimes" })).toBeInTheDocument();
    expect(screen.queryByText(/online|offline/)).not.toBeInTheDocument();
  });
});

describe("TeamPanel member directory", () => {
  function memberRow(role: "owner" | "admin" | "member") {
    return {
      id: "member-1",
      workspace_id: WS,
      user_id: "user-1",
      role,
      created_at: new Date(NOW).toISOString(),
      name: "Ada",
      email: "ada@example.com",
      avatar_url: null,
    };
  }

  function signIn() {
    auth.setState({
      user: {
        id: "user-1",
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

  it("hides the workspace-wide member count from plain members", () => {
    signIn();
    qc.setQueryData(memberListOptions(WS).queryKey, [memberRow("member")]);
    renderPanel([]);

    expect(screen.queryByRole("link", { name: /1 member/ })).not.toBeInTheDocument();
  });

  it("keeps the member directory for workspace admins", () => {
    signIn();
    qc.setQueryData(memberListOptions(WS).queryKey, [memberRow("admin")]);
    renderPanel([]);

    expect(screen.getByRole("link", { name: /1 member/ })).toBeInTheDocument();
  });

  it("keeps the team lens for a workspace owner who leads a department", () => {
    signIn();
    qc.setQueryData(memberListOptions(WS).queryKey, [memberRow("owner")]);
    qc.setQueryData(orgTreeOptions(WS).queryKey, {
      units: [{
        id: "unit-rd",
        parent_id: "",
        name: "Engineering",
        lead_user_id: "user-1",
        can_manage: true,
        description: "",
        position: 0,
      }],
      agents: [],
      members: [{
        user_id: "user-1",
        unit_id: "unit-rd",
        name: "Ada",
        email: "ada@example.com",
        avatar_url: null,
        in_project: true,
      }],
      squads: [],
      can_manage: false,
      my_unit_id: "unit-rd",
    });
    renderPanel([]);

    expect(screen.getByRole("link", { name: /Team agents/ })).toBeInTheDocument();
  });
});

describe("TeamPanel in a project of the organization", () => {
  it("keeps the org chart inside the project", () => {
    const [project] = qc.getQueryData(workspaceListOptions().queryKey) ?? [];
    qc.setQueryData(workspaceListOptions().queryKey, [
      project!,
      { ...project!, id: "ws-org", slug: "org", name: "Org", is_organization: true },
    ]);
    renderPanel([]);

    expect(screen.getByRole("link", { name: /Organization/ })).toHaveAttribute(
      "href",
      "/acme/org",
    );
  });
});
