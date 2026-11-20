// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const api = vi.hoisted(() => ({
  listProjectionCandidates: vi.fn(),
  createAgentProjections: vi.fn(),
  listMembers: vi.fn(),
  getBaseUrl: () => "",
}));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/api", () => ({
  api,
  errorCode: (error: unknown) => (error as { code?: string })?.code,
}));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("sonner", () => ({ toast }));

import { AddFromOrganizationDialog } from "./add-from-organization-dialog";

function candidate(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    name: `Agent ${id}`,
    description: "",
    avatar_url: null,
    owner_id: "user-1",
    is_primary: false,
    projected: false,
    ...overrides,
  };
}

function renderDialog(onOpenChange = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AddFromOrganizationDialog wsId="ws-1" open onOpenChange={onOpenChange} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return onOpenChange;
}

beforeEach(() => {
  vi.clearAllMocks();
  api.listMembers.mockResolvedValue([{ user_id: "user-1", name: "Ada" }]);
});

describe("AddFromOrganizationDialog", () => {
  it("lists only agents not yet in the project and adds the selected ones", async () => {
    api.listProjectionCandidates.mockResolvedValue([
      candidate("a", { is_primary: true }),
      candidate("b", { projected: true }),
      candidate("c"),
    ]);
    api.createAgentProjections.mockResolvedValue([{ id: "p-a" }]);
    const onOpenChange = renderDialog();

    expect(await screen.findByText("Agent a")).toBeInTheDocument();
    expect(screen.queryByText("Agent b")).toBeNull();
    expect(await screen.findByText("Ada's digital twin")).toBeInTheDocument();

    const confirm = screen.getByRole("button", { name: "Add 0 agents" });
    expect(confirm).toBeDisabled();

    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Add 1 agent" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(api.createAgentProjections).toHaveBeenCalledWith(["a"]);
    expect(toast.success).toHaveBeenCalledWith("Added 1 agent");
  });

  it("shows the empty state when every agent is already projected", async () => {
    api.listProjectionCandidates.mockResolvedValue([candidate("a", { projected: true })]);
    renderDialog();

    expect(
      await screen.findByText("Every agent owned by this project's members is already here."),
    ).toBeInTheDocument();
  });

  it("explains a missing organization workspace", async () => {
    api.listProjectionCandidates.mockRejectedValue({ code: "organization_missing" });
    renderDialog();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "There is no organization workspace yet.",
    );
  });
});
