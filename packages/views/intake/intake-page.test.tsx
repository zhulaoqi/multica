import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const intakeApi = vi.hoisted(() => ({
  intakeSession: vi.fn(async () => ({ id: "user-1", name: "周可" })),
  listMyIntake: vi.fn(async () => []),
  listScheduledIntake: vi.fn(async () => []),
  listProjectIntake: vi.fn(async () => []),
  submitIntake: vi.fn(),
  listIntakeAgents: vi.fn(async () => []),
  logout: vi.fn(async () => undefined),
}));

vi.mock("@multica/core/api", () => ({ api: intakeApi }));

import { IntakePage } from "./intake-page";

function renderPage(props: { focusIssueId?: string; workspaceName?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <IntakePage {...props} />
    </QueryClientProvider>,
  );
}

describe("IntakePage", () => {
  beforeEach(() => {
    intakeApi.listMyIntake.mockReset();
    intakeApi.listMyIntake.mockResolvedValue([]);
    intakeApi.listProjectIntake.mockReset();
    intakeApi.listProjectIntake.mockResolvedValue([]);
    intakeApi.listScheduledIntake.mockReset();
    intakeApi.listScheduledIntake.mockResolvedValue([]);
    intakeApi.submitIntake.mockReset();
  });

  it("opens over plain HTTP, where crypto.randomUUID is missing", async () => {
    const realCrypto = globalThis.crypto;
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array<ArrayBuffer>) => realCrypto.getRandomValues(bytes),
    });
    try {
      renderPage();
      expect(await screen.findByPlaceholderText("写下几句话")).toBeTruthy();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("opens in chat without a project, module, or agent picker", async () => {
    renderPage();
    expect(await screen.findByPlaceholderText("写下几句话")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "周可" }));
    expect(screen.getByRole("button", { name: "退出" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "发送" })).toBeTruthy();
    expect(screen.queryByPlaceholderText("项目")).toBeNull();
    expect(screen.queryByPlaceholderText("模块")).toBeNull();
    expect(screen.queryByText("结算助手")).toBeNull();
  });

  it("keeps the text and asks for another sentence when nothing matches", async () => {
    intakeApi.submitIntake.mockResolvedValue({ issue_id: "" });
    renderPage();
    const box = await screen.findByPlaceholderText("写下几句话");
    fireEvent.change(box, { target: { value: "帮我看看结算" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    expect(await screen.findByText("没对上，请再补一句")).toBeTruthy();
    expect((box as HTMLTextAreaElement).value).toBe("帮我看看结算");
  });

  it("shows the three blocker sentences with continue and stop", async () => {
    intakeApi.listMyIntake.mockResolvedValue([
      {
        issue_id: "issue-1",
        body: "补上导出",
        display_status: "等你补充",
        blocker: {
          done: "列表已经出来了",
          missing: "导出格式没说",
          consequence: "按表格导出可能不对",
          state: "open",
        },
        can_continue: true,
        can_stop: true,
      },
    ]);
    renderPage();
    expect(await screen.findByText("已经做了：列表已经出来了")).toBeTruthy();
    expect(screen.getByText("缺什么：导出格式没说")).toBeTruthy();
    expect(screen.getByText("不补会怎样：按表格导出可能不对")).toBeTruthy();
    expect(screen.getByRole("button", { name: "继续" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "停止" })).toBeTruthy();
    expect(screen.queryByText(/agent/i)).toBeNull();
  });

  it("opens a deep link on that request and shows the workspace in the menu", async () => {
    intakeApi.listMyIntake.mockResolvedValue([
      { issue_id: "issue-1", body: "补上导出", display_status: "正在做" },
    ]);
    renderPage({ focusIssueId: "issue-1", workspaceName: "结算组" });
    expect(await screen.findByText("补上导出")).toBeTruthy();
    expect(document.getElementById("intake-issue-1")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "周可" }));
    expect(screen.getByText("结算组")).toBeTruthy();
    expect(screen.getByRole("button", { name: "退出" })).toBeTruthy();
  });

  it("disables send while the request is in flight and shows acceptance", async () => {
    let release: () => void = () => {};
    intakeApi.submitIntake.mockReturnValue(new Promise((resolve) => {
      release = () => resolve({ issue_id: "issue-1" });
    }));
    renderPage();
    const box = await screen.findByPlaceholderText("写下几句话");
    fireEvent.change(box, { target: { value: "补导出" } });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    expect(screen.getByRole("button", { name: "发送" })).toHaveProperty("disabled", true);
    release();
    expect(await screen.findByText("正在做")).toBeTruthy();
  });

  it("shows a retry hint while the same unit is trying again", async () => {
    intakeApi.listMyIntake.mockResolvedValue([
      {
        issue_id: "retry",
        body: "导出",
        display_status: "正在做",
        progress: { done: "这一轮没有新输出，正在同一台重试", remain: "重试之后继续" },
      },
    ]);
    renderPage();
    expect(await screen.findByText(/正在做 · 正在重试/)).toBeTruthy();
  });

  it("says the machine has not started instead of a maintenance remainder", async () => {
    intakeApi.listMyIntake.mockResolvedValue([
      {
        issue_id: "wait",
        body: "导出",
        display_status: "等待处理",
        progress: { done: "还没开始", remain: "需要维护" },
        can_resume: true,
      },
    ]);
    renderPage();
    expect(await screen.findByText("还没开始，恢复后可以继续")).toBeTruthy();
    expect(screen.queryByText("还剩：需要维护")).toBeNull();
    expect(screen.getByRole("button", { name: "继续" })).toBeTruthy();
  });

  it("keeps a future start waiting and shows the accept action", async () => {
    intakeApi.listMyIntake.mockResolvedValue([
      {
        issue_id: "later",
        body: "凌晨再做",
        display_status: "等待处理",
        start_at: "2026-11-12T01:00:00+08:00",
      },
      {
        issue_id: "review",
        body: "等验收",
        display_status: "待验收",
        can_accept: true,
        result_summary: "导出已经接上",
      },
      {
        issue_id: "running",
        body: "正在导出",
        display_status: "正在做",
        run: { status: "running", started_at: new Date(Date.now() - 5 * 60_000).toISOString() },
      },
    ]);
    renderPage();
    expect(await screen.findByText(/开始：2026-11-12/)).toBeTruthy();
    expect(screen.getByText("等待处理")).toBeTruthy();
    expect(screen.getByRole("button", { name: "验收" })).toBeTruthy();
    expect(screen.getByText(/已执行/)).toBeTruthy();
    expect(screen.queryByText(/执行：/)).toBeNull();
  });

  it("returns to chat after refresh and opens help from the logo", async () => {
    renderPage();
    await screen.findByPlaceholderText("写下几句话");
    intakeApi.listProjectIntake.mockResolvedValue([
      {
        issue_id: "p1",
        body: "别人的导出",
        display_status: "正在做",
        project_id: "proj",
        project_title: "结算",
        mine: false,
        can_decide: false,
        elapsed: "12 分钟",
      },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "管理" }));
    expect(await screen.findByText("我的 Agent")).toBeTruthy();
    expect(screen.queryByText("能力总览")).toBeNull();
    expect(await screen.findByText("还没有要到点执行的需求")).toBeTruthy();
    expect(await screen.findByText("别人的导出")).toBeTruthy();
    expect(screen.getByText(/已执行 12 分钟/)).toBeTruthy();
    expect(screen.queryByText(/执行：/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "收口" }));
    expect(screen.getByText(/同一个分支上做完/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "聊天" }));
    await waitFor(() => {
      expect(screen.getByPlaceholderText("写下几句话")).toBeTruthy();
    });
  });
});
