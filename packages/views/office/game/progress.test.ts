// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  COMPLETIONS_PER_LEVEL,
  agentProgress,
  companyProgress,
  completionsInWindow,
  pickMvp,
} from "./progress";

describe("agentProgress", () => {
  it("starts everyone at level 1", () => {
    expect(agentProgress(0)).toEqual({
      level: 1,
      intoLevel: 0,
      levelSize: COMPLETIONS_PER_LEVEL,
    });
  });

  it("levels up every five completions", () => {
    expect(agentProgress(4).level).toBe(1);
    expect(agentProgress(5)).toMatchObject({ level: 2, intoLevel: 0 });
    expect(agentProgress(12)).toMatchObject({ level: 3, intoLevel: 2 });
  });

  it("treats missing or negative counts as zero", () => {
    expect(agentProgress(Number.NaN).level).toBe(1);
    expect(agentProgress(-3).level).toBe(1);
  });
});

describe("companyProgress", () => {
  it("sums contributions and levels the workspace", () => {
    expect(companyProgress([{ completed: 8 }, { completed: 7 }, { completed: 5 }])).toMatchObject({
      completed: 20,
      level: 2,
      intoLevel: 5,
    });
  });

  it("is level 1 with an empty team", () => {
    expect(companyProgress([])).toMatchObject({ completed: 0, level: 1 });
  });
});

describe("completionsInWindow", () => {
  it("sums daily buckets", () => {
    expect(
      completionsInWindow({
        buckets: [{ completed: 2 }, { completed: 0 }, { completed: 3 }],
      }),
    ).toBe(5);
  });

  it("reads missing activity as zero", () => {
    expect(completionsInWindow(undefined)).toBe(0);
  });
});

describe("pickMvp", () => {
  it("crowns the busiest agent", () => {
    expect(
      pickMvp([
        { id: "agent-a", completed: 3 },
        { id: "agent-b", completed: 9 },
      ]),
    ).toBe("agent-b");
  });

  it("breaks ties on id so the crown stays put", () => {
    expect(
      pickMvp([
        { id: "agent-b", completed: 4 },
        { id: "agent-a", completed: 4 },
      ]),
    ).toBe("agent-a");
  });

  it("returns null when nobody has finished anything", () => {
    expect(pickMvp([{ id: "agent-a", completed: 0 }])).toBeNull();
    expect(pickMvp([])).toBeNull();
  });
});
