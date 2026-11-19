/**
 * Digital-twin selection shared by the org tree, the workbench binding gate
 * and the agent settings panel.
 *
 * A member may own several agents. The server marks exactly one active agent
 * per owner as primary; everything that needs "this person's twin" reads that
 * flag. Older servers omit `is_primary`, so the first owned agent is the
 * compatible fallback.
 */
export interface TwinCandidate {
  owner_id?: string | null;
  is_primary?: boolean;
}

/** Every active agent the member owns, primary first. */
export function ownedTwins<T extends TwinCandidate>(
  agents: readonly T[],
  userId: string | null | undefined,
): T[] {
  if (!userId) return [];
  const owned = agents.filter((agent) => agent.owner_id === userId);
  return [...owned.filter((agent) => agent.is_primary === true), ...owned.filter((agent) => agent.is_primary !== true)];
}

/** The member's primary agent in this list, if they have one. */
export function findPrimaryTwin<T extends TwinCandidate>(
  agents: readonly T[],
  userId: string | null | undefined,
): T | null {
  return ownedTwins(agents, userId)[0] ?? null;
}

/** Whether the member is bound: they own an active agent in this workspace. */
export function hasPrimaryTwin(
  agents: readonly TwinCandidate[],
  userId: string | null | undefined,
): boolean {
  return findPrimaryTwin(agents, userId) !== null;
}
