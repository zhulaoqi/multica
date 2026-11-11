import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { HelpPanel } from "./help-panel";

describe("HelpPanel", () => {
  it("hides the management section from a member and keeps it for a lead", () => {
    const onClose = vi.fn();
    const { rerender } = render(<HelpPanel canMaintain={false} onClose={onClose} />);
    expect(screen.queryByText(/能力总览/)).toBeNull();
    rerender(<HelpPanel canMaintain onClose={onClose} />);
    expect(screen.getByText(/能力总览/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(onClose).toHaveBeenCalled();
  });
});
