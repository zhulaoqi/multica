import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  isManager: true,
  sources: {
    sources: [] as Array<{ type: "user" | "org_unit"; id: string }>,
    inherited: true,
    unit_id: "unit-1",
    unit_path: "Engineering / Payments",
  },
  candidates: {
    units: [{ id: "unit-1", path: "Engineering / Payments" }],
    members: [{ id: "user-9", name: "Grace" }],
  },
  save: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ isManager: state.isManager }),
}));

vi.mock("@multica/core/workspace", () => ({
  projectIntakeSourcesOptions: () => ({ queryKey: ["project-intake", "sources"] }),
  projectIntakeCandidatesOptions: () => ({ queryKey: ["project-intake", "candidates"] }),
  useReplaceProjectIntakeSources: () => ({ mutate: state.save }),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQuery: (options: { queryKey?: readonly unknown[] }) =>
    options.queryKey?.[1] === "candidates"
      ? { data: state.candidates, isLoading: false, isError: false }
      : { data: state.sources, isLoading: false, isError: false },
}));

import { ProjectIntakeTab } from "./project-intake-tab";

beforeEach(() => {
  state.isManager = true;
  state.sources = {
    sources: [],
    inherited: true,
    unit_id: "unit-1",
    unit_path: "Engineering / Payments",
  };
  state.save.mockReset();
});

describe("ProjectIntakeTab", () => {
  it("follows the owning department while the project lists nobody", () => {
    renderWithI18n(<ProjectIntakeTab />);

    expect(
      screen.getByText('Following the list of "Engineering / Payments"'),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add/ })).toBeInTheDocument();
  });

  it("saves a person picked from the organization roster", async () => {
    renderWithI18n(<ProjectIntakeTab />);

    await userEvent.click(screen.getByRole("button", { name: /Add/ }));
    await userEvent.click(await screen.findByRole("option", { name: /Grace/ }));

    expect(state.save).toHaveBeenCalledWith([{ type: "user", id: "user-9" }], expect.anything());
  });

  it("saves a department picked from the organization tree", async () => {
    renderWithI18n(<ProjectIntakeTab />);

    await userEvent.click(screen.getByRole("button", { name: /Add/ }));
    await userEvent.click(
      await screen.findByRole("option", { name: /Engineering \/ Payments/ }),
    );

    expect(state.save).toHaveBeenCalledWith(
      [{ type: "org_unit", id: "unit-1" }],
      expect.anything(),
    );
  });

  it("removes an entry and saves the remaining list", async () => {
    state.sources = {
      sources: [{ type: "user", id: "user-9" }],
      inherited: false,
      unit_id: "unit-1",
      unit_path: "Engineering / Payments",
    };
    renderWithI18n(<ProjectIntakeTab />);

    expect(screen.getByText("Grace")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Remove Grace" }));

    expect(state.save).toHaveBeenCalledWith([], expect.anything());
  });

  it("reports a failed save instead of silently dropping the change", async () => {
    state.save.mockImplementation((_next, options?: { onError?: () => void }) => {
      options?.onError?.();
    });
    renderWithI18n(<ProjectIntakeTab />);

    await userEvent.click(screen.getByRole("button", { name: /Add/ }));
    await userEvent.click(await screen.findByRole("option", { name: /Grace/ }));

    expect(screen.getByRole("alert")).toHaveTextContent("Could not save the intake list.");
  });

  it("shows members a read-only list", () => {
    state.isManager = false;
    state.sources = {
      sources: [{ type: "user", id: "user-9" }],
      inherited: false,
      unit_id: "unit-1",
      unit_path: "Engineering / Payments",
    };
    renderWithI18n(<ProjectIntakeTab />);

    expect(screen.getByText("Grace")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove Grace" })).not.toBeInTheDocument();
  });
});
