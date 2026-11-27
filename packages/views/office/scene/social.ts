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
 *
 * When `visitorIds` is supplied, only pairs with at least one eligible
 * visitor are considered; if just the second candidate is eligible the pair
 * is flipped so the idle agent is the one who walks over.
 */
export function pickVisitPair(
  candidates: readonly SocialCandidate[],
  radius: number,
  visitorIds?: ReadonlySet<string>,
): SocialPair | null {
  let best: SocialPair | null = null;
  let bestDistance = radius;

  for (let first = 0; first < candidates.length; first++) {
    const firstCandidate = candidates[first]!;
    for (let second = first + 1; second < candidates.length; second++) {
      const secondCandidate = candidates[second]!;
      const distance = Math.hypot(
        secondCandidate.x - firstCandidate.x,
        secondCandidate.y - firstCandidate.y,
      );
      if (distance >= bestDistance) continue;

      let visitor = firstCandidate;
      let host = secondCandidate;
      if (visitorIds) {
        const firstCanVisit = visitorIds.has(visitor.id);
        const secondCanVisit = visitorIds.has(host.id);
        if (!firstCanVisit && !secondCanVisit) continue;
        if (!firstCanVisit) {
          visitor = secondCandidate;
          host = firstCandidate;
        }
      }

      best = { visitor, host };
      bestDistance = distance;
    }
  }

  return best;
}
