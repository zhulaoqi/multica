import { paths, resolvePostAuthDestination } from "@multica/core/paths";
import type { Workspace } from "@multica/core/types";

/**
 * Where web sends a signed-in visitor with no explicit `next=`.
 *
 * Onboarded members with a workspace land in the C-end, where everyone works;
 * the admin side is reached from its account menu. Pre-onboarding and
 * no-workspace visitors keep the shared resolver. Desktop still uses the
 * shared resolver until its tab model has a home tab.
 */
export function resolveWebPostAuthDestination(
  workspaces: Workspace[],
  hasOnboarded: boolean,
): string {
  if (hasOnboarded && workspaces.length > 0) return paths.home();
  return resolvePostAuthDestination(workspaces, hasOnboarded);
}
