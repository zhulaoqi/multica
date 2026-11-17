/**
 * Office game numbers, derived from real 30-day activity. Levels are a
 * cosmetic read of how much work an agent has landed; they never gate
 * anything a user can do.
 */

/** Completions an agent needs to move up one level. */
export const COMPLETIONS_PER_LEVEL = 5;
/** Completions the workspace needs to move the company up one level. */
export const COMPANY_COMPLETIONS_PER_LEVEL = 15;

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

export interface Contributor {
  id: string;
  completed: number;
}

/** Completed runs across an activity window; absent data counts as zero. */
export function completionsInWindow(
  activity: { buckets: readonly { completed: number }[] } | undefined,
): number {
  if (!activity) return 0;
  return activity.buckets.reduce((sum, bucket) => sum + bucket.completed, 0);
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
