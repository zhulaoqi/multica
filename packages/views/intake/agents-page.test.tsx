import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

import { AgentRoster } from "./agents-page";

describe("AgentRoster", () => {
  it("shows duty, online state, and the debug link", () => {
    render(
      <AgentRoster
        slug="acme"
        agents={[{ id: "a1", name: "结算助手", can: "导出", runtime_mode: "local", online: true, active_count: 2, placements: [{ project: "结算", product: "账", module: "导出", role: "主责" }] }]}
        onSave={vi.fn()}
      />,
    );
    expect(screen.getByText(/结算助手/)).toBeTruthy();
    expect(screen.getByText(/正在做 2 条/)).toBeTruthy();
    expect(screen.getByText(/导出 · 主责/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "调试" }).getAttribute("href")).toBe("/acme/agents/a1");
  });
});
