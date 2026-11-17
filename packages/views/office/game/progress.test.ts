// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  COMPLETIONS_PER_LEVEL,
  WEEKLY_GOAL_FLOOR,
  WEEKLY_WINDOW_DAYS,
  agentProgress,
  companyProgress,
  completionsInLastDays,
  completionsInWindow,
  completionsToday,
  pickMvp,
  weeklyGoal,
  weeklyProgress,
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

describe("completionsInLastDays", () => {
  const activity = {
    buckets: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map((completed) => ({ completed })),
  };

  it("reads the newest tail of the series", () => {
    expect(completionsInLastDays(activity)).toBe(4 + 5 + 6 + 7 + 8 + 9 + 10);
    expect(completionsInLastDays(activity, 3)).toBe(8 + 9 + 10);
  });

  it("does not overflow a short series", () => {
    expect(completionsInLastDays({ buckets: [{ completed: 2 }] })).toBe(2);
    expect(completionsInLastDays(undefined)).toBe(0);
  });

  it("uses the weekly window by default", () => {
    expect(WEEKLY_WINDOW_DAYS).toBe(7);
  });
});

describe("completionsToday", () => {
  it("reads the newest bucket only", () => {
    expect(
      completionsToday({ buckets: [{ completed: 2 }, { completed: 5 }] }),
    ).toBe(5);
  });

  it("is zero without activity", () => {
    expect(completionsToday(undefined)).toBe(0);
    expect(completionsToday({ buckets: [] })).toBe(0);
  });
});

describe("weeklyGoal", () => {
  it("scales with the team and keeps a floor", () => {
    expect(weeklyGoal(0)).toBe(WEEKLY_GOAL_FLOOR);
    expect(weeklyGoal(1)).toBe(WEEKLY_GOAL_FLOOR);
    expect(weeklyGoal(4)).toBe(12);
  });
});

describe("weeklyProgress", () => {
  const week = (completed: number) => ({ buckets: [{ completed }] });

  it("sums every agent's recent work", () => {
    expect(weeklyProgress([week(3), week(4), undefined], 2)).toEqual({
      completed: 7,
      goal: WEEKLY_GOAL_FLOOR,
      met: true,
    });
  });

  it("does not celebrate a goal nobody has touched", () => {
    expect(weeklyProgress([], 1)).toEqual({
      completed: 0,
      goal: WEEKLY_GOAL_FLOOR,
      met: false,
    });
  });

  it("keeps the goal unmet until the team reaches it", () => {
    expect(weeklyProgress([week(2), week(1)], 3)).toEqual({
      completed: 3,
      goal: 9,
      met: false,
    });
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
