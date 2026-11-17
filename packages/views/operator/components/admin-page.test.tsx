import { describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import type { OperatorOverview } from "@multica/core/operator";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AdminPage } from "./admin-page";

const queryState = vi.hoisted(() => ({
  data: undefined as OperatorOverview | undefined,
  isPending: false,
  isError: false,
  isFetching: false,
  refetch: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useQuery: (options: { queryKey?: readonly unknown[] }) =>
    options?.queryKey?.includes("directories")
      ? {
          data: { configured: true, schedule: "", workspaces: [] },
          isPending: false,
          isError: false,
          isFetching: false,
          refetch: vi.fn(),
        }
      : queryState,
}));

vi.mock("@multica/core/operator", () => ({
  operatorOverviewOptions: () => ({ queryKey: ["operator", "overview"] }),
  operatorDirectoryOptions: () => ({ queryKey: ["operator", "directories"] }),
  operatorKeys: { directories: () => ["operator", "directories"] },
}));

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/admin",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `http://test${path}`,
};

function renderPage() {
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <AdminPage />
    </NavigationProvider>,
  );
}

const OVERVIEW: OperatorOverview = {
  generated_at: "2026-11-13T00:00:00Z",
  workspaces: [
    {
      id: "ws-1",
      slug: "acme",
      name: "Acme",
      created_at: "2026-01-01T00:00:00Z",
      owner_emails: ["owner@acme.test"],
      owner_count: 1,
      admin_count: 0,
      member_count: 2,
      agent_count: 3,
      channels: [
        { type: "feishu", active: 1, revoked: 0 },
        { type: "slack", active: 0, revoked: 1 },
      ],
      usage_7d: {
        task_count: 12,
        input_tokens: 1_500,
        output_tokens: 500,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cost_usd_ticks: 15_000_000_000,
      },
    },
    {
      id: "ws-2",
      slug: "globex",
      name: "Globex",
      created_at: "2026-02-01T00:00:00Z",
      owner_emails: [],
      owner_count: 0,
      admin_count: 0,
      member_count: 0,
      agent_count: 0,
      channels: [],
      usage_7d: {
        task_count: 0,
        input_tokens: 0,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cost_usd_ticks: 0,
      },
    },
  ],
};

describe("AdminPage", () => {
  it("renders one row per workspace with channels, usage and links into settings", () => {
    queryState.data = OVERVIEW;
    renderPage();

    expect(screen.getByRole("link", { name: "Back to workspace" }).getAttribute("href")).toBe(
      "/acme/workbench",
    );

    const acme = screen.getByRole("row", { name: /Acme/ });
    expect(within(acme).getByText("owner@acme.test")).toBeTruthy();
    expect(within(acme).getByText("1 owners · 0 admins · 2 members")).toBeTruthy();
    expect(within(acme).getByText("Feishu 1")).toBeTruthy();
    expect(within(acme).getByText("1 revoked")).toBeTruthy();
    expect(within(acme).getByText("$1.50")).toBeTruthy();
    expect(within(acme).getByText("12 tasks · 2K tokens")).toBeTruthy();

    expect(within(acme).getByRole("link", { name: "Channels" }).getAttribute("href")).toBe(
      "/acme/settings?tab=channels",
    );
    expect(within(acme).getByRole("link", { name: "Members" }).getAttribute("href")).toBe(
      "/acme/settings?tab=members",
    );
    expect(within(acme).getByRole("link", { name: "Usage" }).getAttribute("href")).toBe(
      "/acme/usage",
    );

    const globex = screen.getByRole("row", { name: /Globex/ });
    expect(within(globex).getAllByText("None").length).toBeGreaterThan(0);
  });

  it("sums the cost card across workspaces", () => {
    queryState.data = OVERVIEW;
    renderPage();

    // The summary card repeats the only workspace that spent anything.
    expect(screen.getAllByText("$1.50")).toHaveLength(2);
    expect(screen.getAllByText("2K").length).toBeGreaterThanOrEqual(1);
  });

  it("shows the empty state when there are no workspaces", () => {
    queryState.data = { generated_at: "", workspaces: [] };
    renderPage();

    expect(screen.getByText("No workspaces yet.")).toBeTruthy();
  });

  it("surfaces a failed request with a retry action", () => {
    queryState.data = undefined;
    queryState.isError = true;
    renderPage();

    expect(screen.getByText("Could not load the overview.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    queryState.isError = false;
  });
});
