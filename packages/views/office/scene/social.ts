/**
 * Pair selection for walk-over conversations, kept pure so the scheduler in
 * `agents.ts` only deals with animation and pathfinding.
 */

export interface SocialCandidate {
  id: string;
  x: number;
  y: number;
}

export interface SocialPair {
  visitor: SocialCandidate;
  host: SocialCandidate;
}

/**
 * The closest pair of agents within `radius`. Ties keep the earlier pair in
 * the input order, so a stable agent list produces the same visit each time.
 */
export function pickVisitPair(
  candidates: readonly SocialCandidate[],
  radius: number,
): SocialPair | null {
  let best: SocialPair | null = null;
  let bestDistance = radius;

  for (let first = 0; first < candidates.length; first++) {
    const visitor = candidates[first]!;
    for (let second = first + 1; second < candidates.length; second++) {
      const host = candidates[second]!;
      const distance = Math.hypot(host.x - visitor.x, host.y - visitor.y);
      if (distance < bestDistance) {
        best = { visitor, host };
        bestDistance = distance;
      }
    }
  }

  return best;
}
