/**
 * Office game numbers, derived from real 30-day activity. Levels are a
 * cosmetic read of how much work an agent has landed; they never gate
 * anything a user can do.
 */

/** Completions an agent needs to move up one level. */
export const COMPLETIONS_PER_LEVEL = 5;
/** Completions the workspace needs to move the company up one level. */
export const COMPANY_COMPLETIONS_PER_LEVEL = 15;
/** Days of work the shared weekly goal counts. */
export const WEEKLY_WINDOW_DAYS = 7;
/** Every agent in the workspace adds this much to the weekly goal. */
export const WEEKLY_COMPLETIONS_PER_AGENT = 3;
/** Small teams still share a visible target. */
export const WEEKLY_GOAL_FLOOR = 6;

export interface AgentProgress {
  level: number;
  /** Completions inside the current level. */
  intoLevel: number;
  /** Completions needed to leave the current level. */
  levelSize: number;
}

export interface CompanyProgress extends AgentProgress {
  /** Total completions across the workspace window. */
  completed: number;
}

function levelOf(completed: number, perLevel: number): AgentProgress {
  const safe = Number.isFinite(completed) ? Math.max(0, Math.floor(completed)) : 0;
  return {
    level: Math.floor(safe / perLevel) + 1,
    intoLevel: safe % perLevel,
    levelSize: perLevel,
  };
}

export function agentProgress(completed: number): AgentProgress {
  return levelOf(completed, COMPLETIONS_PER_LEVEL);
}

export function companyProgress(
  contributors: readonly { completed: number }[],
): CompanyProgress {
  const completed = contributors.reduce(
    (sum, contributor) => sum + Math.max(0, contributor.completed),
    0,
  );
  return { ...levelOf(completed, COMPANY_COMPLETIONS_PER_LEVEL), completed };
}

interface ActivityLike {
  buckets: readonly { completed: number }[];
}

export interface Contributor {
  id: string;
  completed: number;
}

/** Completed runs across an activity window; absent data counts as zero. */
export function completionsInWindow(
  activity: ActivityLike | undefined,
): number {
  if (!activity) return 0;
  return activity.buckets.reduce((sum, bucket) => sum + Math.max(0, bucket.completed), 0);
}

/**
 * Completions across the newest `days` buckets. Buckets are oldest → newest,
 * so this reads the tail: the front desk reports what the team landed
 * recently, not what it landed a month ago.
 */
export function completionsInLastDays(
  activity: ActivityLike | undefined,
  days = WEEKLY_WINDOW_DAYS,
): number {
  if (!activity || !(days > 0)) return 0;
  const start = Math.max(0, activity.buckets.length - Math.floor(days));
  let sum = 0;
  for (let index = start; index < activity.buckets.length; index++) {
    sum += Math.max(0, activity.buckets[index]!.completed);
  }
  return sum;
}

/** The newest bucket is today. */
export function completionsToday(activity: ActivityLike | undefined): number {
  if (!activity || activity.buckets.length === 0) return 0;
  return Math.max(0, activity.buckets[activity.buckets.length - 1]!.completed);
}

export interface WeeklyProgress {
  completed: number;
  goal: number;
  met: boolean;
}

/** A target the whole office can reach together, scaled by team size. */
export function weeklyGoal(activeAgents: number): number {
  const agents = Number.isFinite(activeAgents) ? Math.max(0, Math.floor(activeAgents)) : 0;
  return Math.max(WEEKLY_GOAL_FLOOR, agents * WEEKLY_COMPLETIONS_PER_AGENT);
}

/**
 * The shared weekly goal. `met` stays false while nothing has landed, so a
 * fresh workspace with a fresh goal does not celebrate at zero.
 */
export function weeklyProgress(
  activities: readonly (ActivityLike | undefined)[],
  activeAgents: number,
): WeeklyProgress {
  const goal = weeklyGoal(activeAgents);
  let completed = 0;
  for (const activity of activities) {
    completed += completionsInLastDays(activity);
  }
  return { completed, goal, met: completed > 0 && completed >= goal };
}

/**
 * Agent id with the most completions in the window, or null when nobody has
 * landed anything. Ties break on id so the crown does not jump between two
 * equally busy agents on every render.
 */
export function pickMvp(contributors: readonly Contributor[]): string | null {
  let best: Contributor | null = null;
  for (const contributor of contributors) {
    if (!(contributor.completed > 0)) continue;
    if (
      !best ||
      contributor.completed > best.completed ||
      (contributor.completed === best.completed && contributor.id < best.id)
    ) {
      best = contributor;
    }
  }
  return best?.id ?? null;
}
