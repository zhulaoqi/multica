// @vitest-environment node

import { describe, expect, it } from "vitest";
import { earnedTrophies, nextTrophy, trophyBoard, type TrophyStats } from "./trophies";

const FRESH: TrophyStats = {
  archived: 0,
  teamSize: 1,
  companyLevel: 1,
  totalCompleted: 0,
  weeklyMet: false,
  mvpLevel: 1,
  departments: 0,
};

describe("trophy wall", () => {
  it("starts empty and points at the first goal", () => {
    expect(earnedTrophies(FRESH)).toEqual([]);
    expect(nextTrophy(FRESH)).toBe("first_delivery");
    expect(trophyBoard(FRESH).every((trophy) => !trophy.earned)).toBe(true);
  });

  it("awards trophies from every stat source", () => {
    const stats: TrophyStats = {
      archived: 1,
      teamSize: 3,
      companyLevel: 3,
      totalCompleted: 50,
      weeklyMet: true,
      mvpLevel: 3,
      departments: 2,
    };
    expect(earnedTrophies(stats)).toEqual([
      "first_delivery",
      "crew",
      "weekly_goal",
      "mvp_level_3",
      "company_3",
      "fifty",
    ]);
    expect(nextTrophy(stats)).toBe("company_5");
  });

  it("keeps a stable display order and earns everything at the top", () => {
    const board = trophyBoard({
      ...FRESH,
      archived: 10,
      teamSize: 10,
      companyLevel: 9,
      totalCompleted: 500,
      weeklyMet: true,
      mvpLevel: 9,
      departments: 6,
    });
    expect(board.map((trophy) => trophy.id)).toEqual([
      "first_delivery",
      "crew",
      "weekly_goal",
      "mvp_level_3",
      "company_3",
      "fifty",
      "company_5",
      "hundred",
    ]);
    expect(board.every((trophy) => trophy.earned)).toBe(true);
    expect(nextTrophy({
      ...FRESH,
      archived: 10,
      teamSize: 10,
      companyLevel: 9,
      totalCompleted: 500,
      weeklyMet: true,
      mvpLevel: 9,
      departments: 6,
    })).toBeNull();
  });
});
