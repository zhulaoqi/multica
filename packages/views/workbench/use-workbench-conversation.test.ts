// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { DispatchListItem } from "@multica/core/workbench";
import { dispatchPhase, type ChildProgress } from "./use-workbench-conversation";

function dispatch(overrides: {
  status?: string;
  statusCategory?: string;
  completed?: boolean;
  leaderState?: string;
}): DispatchListItem {
  const {
    status = "todo",
    statusCategory,
    completed = false,
    leaderState = "none",
  } = overrides;
  return {
    issue: {
      id: "issue-1",
      identifier: "MUL-1",
      title: "Ship the onboarding page",
      description: null,
      status,
      ...(statusCategory ? { status_category: statusCategory } : {}),
      created_at: "2026-11-12T08:00:00Z",
    },
    unit_id: "unit-1",
    unit_path: "Engineering",
    dispatched_at: "2026-11-12T08:00:00Z",
    completed,
    leader_state: leaderState,
  } as unknown as DispatchListItem;
}

const NONE: ChildProgress | undefined = undefined;

describe("dispatchPhase", () => {
  it("reports cancellation before completion signals", () => {
    expect(
      dispatchPhase(dispatch({ status: "cancelled", completed: true }), NONE),
    ).toBe("cancelled");
  });

  it("treats a written receipt as completed", () => {
    expect(dispatchPhase(dispatch({ completed: true }), NONE)).toBe("completed");
  });

  it.each(["done", "closed"])(
    "treats a %s status category as completed even without a receipt",
    (category) => {
      expect(
        dispatchPhase(dispatch({ status: category, statusCategory: category }), NONE),
      ).toBe("completed");
    },
  );

  it("waits on acceptance while the root issue is in review", () => {
    expect(dispatchPhase(dispatch({ status: "in_review" }), NONE)).toBe("in_review");
  });

  it("reports failure only from the leader task", () => {
    expect(
      dispatchPhase(dispatch({ leaderState: "failed" }), NONE),
    ).toBe("failed");
  });

  it("reports decomposition progress once children exist", () => {
    expect(
      dispatchPhase(dispatch({ leaderState: "running" }), { done: 1, total: 3 }),
    ).toBe("progress");
  });

  it("says the leader is decomposing while it runs without children", () => {
    expect(
      dispatchPhase(dispatch({ leaderState: "running" }), { done: 0, total: 0 }),
    ).toBe("decomposing");
  });

  it("falls back to queued when the server has no leader state", () => {
    expect(dispatchPhase(dispatch({ leaderState: "none" }), NONE)).toBe("queued");
  });

  it("prefers an empty child list over a running leader", () => {
    expect(
      dispatchPhase(dispatch({ leaderState: "queued" }), { done: 0, total: 0 }),
    ).toBe("decomposing");
  });
});
