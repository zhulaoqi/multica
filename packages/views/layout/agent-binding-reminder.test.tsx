import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { Agent } from "@multica/core/types";
import { agentListOptions, workspaceListOptions } from "@multica/core/workspace/queries";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { AgentBindingReminder } from "./agent-binding-reminder";

const mocks = vi.hoisted(() => ({
  warning: vi.fn(),
  push: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { warning: mocks.warning },
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  const api = { listAgents: vi.fn().mockResolvedValue([]) };
  return { ...actual, api, getApi: () => api };
});

const NOW = "2026-11-20T08:00:00Z";
const adapter: NavigationAdapter = {
  push: (...args) => mocks.push(...args),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function signIn(userId: string) {
  const auth = createAuthStore({
    api: {} as never,
    storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  });
  auth.setState({ user: { id: userId } as never });
  registerAuthStore(auth);
}

function agent(overrides: Partial<Agent>): Agent {
  return {
    id: "agent-1",
    owner_id: "user-1",
    kind: "user",
    is_primary: true,
    archived_at: null,
    ...overrides,
  } as unknown as Agent;
}

function workspace(id: string, slug: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    name: slug,
    slug,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACM",
    avatar_url: null,
    created_at: NOW,
    updated_at: NOW,
    ...overrides,
  };
}

// The reminder fires once per workspace per session, so each case uses its
// own workspace id. `organization` adds the org workspace next to it, which
// makes the current workspace a project.
function renderReminder(wsId: string, agents: Agent[], organization = false) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(workspaceListOptions().queryKey, [
    workspace(wsId, "acme"),
    ...(organization
      ? [workspace("ws-org", "company", { is_organization: true })]
      : []),
  ]);
  qc.setQueryData(agentListOptions(wsId).queryKey, agents);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <AgentBindingReminder />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("AgentBindingReminder", () => {
  beforeEach(() => {
    mocks.warning.mockReset();
    mocks.push.mockReset();
    signIn("user-1");
  });

  it("reminds an unbound member once and links to the agents page", async () => {
    const view = renderReminder("ws-unbound", [agent({ owner_id: "someone-else" })]);

    await waitFor(() => expect(mocks.warning).toHaveBeenCalledTimes(1));
    const [title, options] = mocks.warning.mock.calls[0]!;
    expect(title).toBe("Bind your agent first");
    expect(options.action.label).toBe("Bind an agent");
    options.action.onClick();
    expect(mocks.push).toHaveBeenCalledWith("/acme/agents");

    view.unmount();
    renderReminder("ws-unbound", []);
    await Promise.resolve();
    expect(mocks.warning).toHaveBeenCalledTimes(1);
  });

  it("links a project member to the organization's agents page", async () => {
    renderReminder("ws-project", [agent({ owner_id: "someone-else" })], true);

    await waitFor(() => expect(mocks.warning).toHaveBeenCalledTimes(1));
    const [, options] = mocks.warning.mock.calls[0]!;
    options.action.onClick();
    expect(mocks.push).toHaveBeenCalledWith("/company/agents");
  });

  it("treats an archived twin as unbound", async () => {
    renderReminder("ws-archived", [agent({ archived_at: NOW })]);
    await waitFor(() => expect(mocks.warning).toHaveBeenCalledTimes(1));
  });

  it("stays quiet for a member with a primary twin", async () => {
    renderReminder("ws-bound", [agent({})]);
    await Promise.resolve();
    expect(mocks.warning).not.toHaveBeenCalled();
  });
});
