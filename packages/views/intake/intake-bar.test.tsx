import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IntakeBar } from "./intake-bar";

describe("IntakeBar", () => {
  it("starts in chat and switches the label when the mode changes", () => {
    const onMode = vi.fn();
    const { rerender } = render(
      <IntakeBar person="周可" workspaceName="结算组" mode="chat" menuOpen={false} onHelp={vi.fn()} onMenu={vi.fn()} onMode={onMode} onSignOut={vi.fn()} />,
    );
    expect(screen.getByRole("button", { name: "管理" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "管理" }));
    expect(onMode).toHaveBeenCalled();
    rerender(
      <IntakeBar person="周可" workspaceName="结算组" mode="manage" menuOpen onHelp={vi.fn()} onMenu={vi.fn()} onMode={onMode} onSignOut={vi.fn()} />,
    );
    expect(screen.getByRole("button", { name: "聊天" })).toBeTruthy();
    expect(screen.getByText("结算组")).toBeTruthy();
    expect(screen.getByRole("button", { name: "退出" })).toBeTruthy();
  });
});
