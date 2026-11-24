// @vitest-environment node

import { describe, expect, it } from "vitest";
import { ApiError } from "../api";
import { parseDispatchCandidates } from "./queries";
import {
  DispatchListItemSchema,
  DispatchTrailSchema,
  DispatchWorkResponseSchema,
} from "./schemas";

describe("parseDispatchCandidates", () => {
  it("reads candidates from an ambiguous routing error", () => {
    const error = new ApiError("ambiguous", 422, "Unprocessable Entity", {
      code: "org_dispatch_ambiguous",
      candidates: [
        { unit_id: "unit-1", path: "Engineering/Payments", agent_count: 3 },
        { unit_id: "unit-2", path: "Engineering/Infra", agent_count: 1 },
      ],
    });

    expect(parseDispatchCandidates(error)).toEqual([
      { unit_id: "unit-1", path: "Engineering/Payments", agent_count: 3 },
      { unit_id: "unit-2", path: "Engineering/Infra", agent_count: 1 },
    ]);
  });

  it("defaults missing candidate fields instead of dropping the candidate", () => {
    const error = new ApiError("ambiguous", 422, "Unprocessable Entity", {
      candidates: [{ unit_id: "unit-1" }],
    });

    expect(parseDispatchCandidates(error)).toEqual([
      { unit_id: "unit-1", path: "", agent_count: 0 },
    ]);
  });

  it("returns nothing for unrelated errors or malformed bodies", () => {
    expect(parseDispatchCandidates(new Error("network"))).toEqual([]);
    expect(
      parseDispatchCandidates(
        new ApiError("server", 500, "Internal Server Error", { code: "org_dispatch_failed" }),
      ),
    ).toEqual([]);
    expect(
      parseDispatchCandidates(
        new ApiError("ambiguous", 422, "Unprocessable Entity", {
          candidates: [{ agent_count: 2 }],
        }),
      ),
    ).toEqual([]);
  });
});

describe("workbench schemas", () => {
  it("keeps unknown leader states so the page can fall back", () => {
    const listItem = DispatchListItemSchema.pick({
      leader_state: true,
      completed: true,
      unit_path: true,
    });
    expect(listItem.parse({ leader_state: "paused" })).toEqual({
      leader_state: "paused",
      completed: false,
      unit_path: "",
    });
  });

  it("rejects a dispatch response without an issue", () => {
    const result = DispatchWorkResponseSchema.safeParse({
      issue: null,
      dispatch: { unit_id: "unit-1" },
    });
    expect(result.success).toBe(false);
  });

  it("defaults a dispatch row from a server that predates projects and progress", () => {
    const parsed = DispatchListItemSchema.parse({
      issue: issueFixture,
      dispatched_at: "2026-11-12T08:00:00Z",
    });

    expect(parsed.project_id).toBe("");
    expect(parsed.progress).toBeUndefined();
    expect(parsed.personal).toBe(false);
  });

  it("reads the progress a cross-workspace dispatch carries", () => {
    const parsed = DispatchListItemSchema.parse({
      issue: { ...issueFixture, workspace_id: "ws-project" },
      dispatched_at: "2026-11-12T08:00:00Z",
      progress: { done: 1, total: 3 },
    });

    expect(parsed.progress).toEqual({ done: 1, total: 3 });
  });

  it("fills a thin trail response with empty defaults", () => {
    const parsed = DispatchTrailSchema.parse({ issue: issueFixture });

    expect(parsed.children).toEqual([]);
    expect(parsed.progress).toEqual({ done: 0, total: 0 });
    expect(parsed.result).toBeUndefined();
    expect(parsed.deliverables).toEqual([]);
  });
});

const issueFixture = {
  id: "issue-1",
  workspace_id: "ws-project",
  number: 7,
  identifier: "AIHUB-7",
  title: "Which models does AIHUB serve?",
  description: null,
  status: "in_progress",
  priority: "medium",
  assignee_type: "agent",
  assignee_id: "agent-1",
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "2026-11-12T08:00:00Z",
  updated_at: "2026-11-12T08:00:00Z",
};
