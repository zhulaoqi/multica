/**
 * The trophy wall is a collection read-out, not a second score. Every trophy
 * derives from stats the page already computes, so it cannot drift from the
 * roster, the front desk, or the office levels.
 */

export type TrophyId =
  | "first_delivery"
  | "crew"
  | "company_3"
  | "company_5"
  | "weekly_goal"
  | "mvp_level_3"
  | "fifty"
  | "hundred";

export interface TrophyStats {
  /** Completed dispatches the requester has seen through. */
  archived: number;
  /** Active agents in the workspace. */
  teamSize: number;
  companyLevel: number;
  /** Completions across the 30-day activity window. */
  totalCompleted: number;
  weeklyMet: boolean;
  /** Level of the busiest agent, or 1 when nobody has landed work. */
  mvpLevel: number;
  /** Departments with a staffed room plaque. */
  departments: number;
}

export interface Trophy {
  id: TrophyId;
  earned: boolean;
}

const CHECKS: Record<TrophyId, (stats: TrophyStats) => boolean> = {
  first_delivery: (stats) => stats.archived >= 1,
  crew: (stats) => stats.teamSize >= 3,
  company_3: (stats) => stats.companyLevel >= 3,
  company_5: (stats) => stats.companyLevel >= 5,
  weekly_goal: (stats) => stats.weeklyMet,
  mvp_level_3: (stats) => stats.mvpLevel >= 3,
  fifty: (stats) => stats.totalCompleted >= 50,
  hundred: (stats) => stats.totalCompleted >= 100,
};

const ORDER: readonly TrophyId[] = [
  "first_delivery",
  "crew",
  "weekly_goal",
  "mvp_level_3",
  "company_3",
  "fifty",
  "company_5",
  "hundred",
];

export function trophyBoard(stats: TrophyStats): Trophy[] {
  return ORDER.map((id) => ({ id, earned: CHECKS[id](stats) }));
}

export function earnedTrophies(stats: TrophyStats): TrophyId[] {
  return ORDER.filter((id) => CHECKS[id](stats));
}

/** Next locked trophy, so the wall can point at the nearest goal. */
export function nextTrophy(stats: TrophyStats): TrophyId | null {
  return ORDER.find((id) => !CHECKS[id](stats)) ?? null;
}
