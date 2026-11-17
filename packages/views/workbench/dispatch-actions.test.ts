// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  canReview,
  followUpComment,
  rejectionComment,
  subjectMention,
} from "./dispatch-actions";

describe("canReview", () => {
  it("accepts only a delivered result", () => {
    expect(canReview("in_review")).toBe(true);
    for (const phase of ["queued", "decomposing", "progress", "completed", "failed", "cancelled"] as const) {
      expect(canReview(phase)).toBe(false);
    }
  });
});

describe("subjectMention", () => {
  it("addresses the assignee in the editor's mention format", () => {
    expect(subjectMention("squad", "s1", "Content")).toBe(
      "[@Content](mention://squad/s1)",
    );
    expect(subjectMention("agent", "a1", "Lead")).toBe(
      "[@Lead](mention://agent/a1)",
    );
  });

  it("escapes brackets so the label round-trips", () => {
    expect(subjectMention("agent", "a1", "QA [web]")).toBe(
      "[@QA \\[web\\]](mention://agent/a1)",
    );
  });

  it("declines to mention without a resolvable assignee", () => {
    expect(subjectMention(null, "x", "Lead")).toBeNull();
    expect(subjectMention("agent", null, "Lead")).toBeNull();
    expect(subjectMention("plugin", "p1", "Bot")).toBeNull();
  });
});

describe("thread replies", () => {
  it("leads the rejection with the mention and keeps the reason", () => {
    expect(rejectionComment("[@Lead](mention://agent/a1)", "  Cover pricing.  ")).toBe(
      "[@Lead](mention://agent/a1)\n\nCover pricing.",
    );
    expect(rejectionComment(null, "Cover pricing.")).toBe("Cover pricing.");
  });

  it("formats a follow-up the same way", () => {
    expect(followUpComment("[@Lead](mention://agent/a1)", " Also add screenshots ")).toBe(
      "[@Lead](mention://agent/a1)\n\nAlso add screenshots",
    );
  });
});
