import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createIssue: vi.fn(),
  push: vi.fn(),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mocks.createIssue, isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: mocks.push }),
}));

import { renderWithI18n } from "../../test/i18n";
import { DispatchDialog } from "./dispatch-dialog";

function renderDialog(onOpenChange = vi.fn()) {
  renderWithI18n(
    <DispatchDialog
      target={{ type: "squad", id: "squad-1", name: "Payments squad" }}
      open
      onOpenChange={onOpenChange}
    />,
  );
  return onOpenChange;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.createIssue.mockResolvedValue({ id: "issue-1" });
});

describe("DispatchDialog", () => {
  it("refuses to dispatch without a goal and acceptance criteria", () => {
    renderDialog();

    fireEvent.change(screen.getByPlaceholderText("What needs to get done?"), {
      target: { value: "Ship payments" },
    });

    expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
    expect(mocks.createIssue).not.toHaveBeenCalled();
  });

  it("sends the handoff contract as the issue description", async () => {
    const onOpenChange = renderDialog();

    fireEvent.change(screen.getByPlaceholderText("What needs to get done?"), {
      target: { value: "Ship payments" },
    });
    fireEvent.change(screen.getByPlaceholderText("The outcome you want."), {
      target: { value: "Users can pay by card" },
    });
    fireEvent.change(screen.getByPlaceholderText("How you'll know it's done."), {
      target: { value: "Checkout test passes" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    await waitFor(() =>
      expect(mocks.createIssue).toHaveBeenCalledWith({
        title: "Ship payments",
        description:
          "## Goal\nUsers can pay by card\n\n## Acceptance\nCheckout test passes",
        status: "todo",
        assignee_type: "squad",
        assignee_id: "squad-1",
      }),
    );
    expect(mocks.push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("shows a failed dispatch instead of navigating away", async () => {
    mocks.createIssue.mockRejectedValue(new Error("issue limit reached"));
    renderDialog();

    fireEvent.change(screen.getByPlaceholderText("What needs to get done?"), {
      target: { value: "Ship payments" },
    });
    fireEvent.change(screen.getByPlaceholderText("The outcome you want."), {
      target: { value: "Users can pay by card" },
    });
    fireEvent.change(screen.getByPlaceholderText("How you'll know it's done."), {
      target: { value: "Checkout test passes" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    expect(await screen.findByText("issue limit reached")).toBeInTheDocument();
    expect(mocks.push).not.toHaveBeenCalled();
  });
});
