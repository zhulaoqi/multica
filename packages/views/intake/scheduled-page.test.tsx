import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ScheduledList } from "./scheduled-page";

describe("ScheduledList", () => {
  it("shows the empty line and cancels a future request", () => {
    const onCancel = vi.fn();
    const { rerender } = render(<ScheduledList items={[]} onCancel={onCancel} />);
    expect(screen.getByText("还没有要到点执行的需求")).toBeTruthy();
    rerender(<ScheduledList items={[{ issue_id: "i1", body: "凌晨导出", display_status: "等待处理", start_at: "2026-11-11T01:00:00Z" }]} onCancel={onCancel} />);
    expect(screen.getByText("凌晨导出")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(onCancel).toHaveBeenCalledWith("i1");
  });
});
