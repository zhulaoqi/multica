// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { agentActivity, type AgentActivityInput } from "./use-agent-activity";

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return { id: "t", agent_id: "a", issue_id: "", status: "running", ...overrides } as AgentTask;
}

function input(overrides: Partial<AgentActivityInput> = {}): AgentActivityInput {
  return {
    status: "working",
    running: [],
    queuedCount: 0,
    issueLabels: new Map([["issue-1", "MAKE-11 Write the launch post"]]),
    ...overrides,
  };
}

describe("agentActivity", () => {
  it("names the one issue a working agent is running", () => {
    expect(agentActivity(input({ running: [task({ issue_id: "issue-1" })] }))).toEqual({
      kind: "issue",
      label: "MAKE-11 Write the launch post",
    });
  });

  it("falls back to working while the issue title is still loading", () => {
    expect(agentActivity(input({ running: [task({ issue_id: "issue-2" })] }))).toEqual({
      kind: "working",
    });
  });

  it("counts several runs instead of naming one", () => {
    expect(
      agentActivity(input({ running: [task({ issue_id: "issue-1" }), task({ id: "t2" })] })),
    ).toEqual({ kind: "running", count: 2 });
  });

  it("tells chats and autopilots apart from issue work", () => {
    expect(agentActivity(input({ running: [task({ chat_session_id: "c" })] }))).toEqual({
      kind: "chat",
    });
    expect(agentActivity(input({ running: [task({ autopilot_run_id: "r" })] }))).toEqual({
      kind: "autopilot",
    });
  });

  it("gives every other status a caption", () => {
    expect(agentActivity(input({ status: "queued", queuedCount: 3 }))).toEqual({
      kind: "queued",
      count: 3,
    });
    expect(agentActivity(input({ status: "idle" }))).toEqual({ kind: "idle" });
    expect(agentActivity(input({ status: "offline" }))).toEqual({ kind: "offline" });
  });
});
