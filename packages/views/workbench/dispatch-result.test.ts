// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { dispatchResult } from "./dispatch-result";

function entry(id: string, overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: "agent-1",
    created_at: "2026-11-12T08:00:00Z",
    comment_type: "comment",
    content: `content ${id}`,
    ...overrides,
  };
}

describe("dispatchResult", () => {
  it("picks the newest agent comment", () => {
    expect(dispatchResult([entry("a"), entry("b")])?.id).toBe("b");
  });

  it("skips members, activities, platform notices, tombstones and blank comments", () => {
    const entries = [
      entry("result"),
      entry("member", { actor_type: "member" }),
      entry("activity", { type: "activity", content: undefined }),
      entry("system", { comment_type: "system" }),
      entry("status", { comment_type: "status_change" }),
      entry("deleted", { deleted_at: "2026-11-12T09:00:00Z" }),
      entry("blank", { content: "   " }),
    ];
    expect(dispatchResult(entries)?.id).toBe("result");
  });

  it("returns null when no agent delivered anything", () => {
    expect(dispatchResult([entry("m", { actor_type: "member" })])).toBeNull();
    expect(dispatchResult([])).toBeNull();
  });

  it("accepts progress updates as results", () => {
    expect(dispatchResult([entry("p", { comment_type: "progress_update" })])?.id).toBe("p");
  });
});
