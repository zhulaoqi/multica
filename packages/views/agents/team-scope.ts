// Structural snapshot: the hook only needs unit ids the viewer can manage
// and which members sit in them, so tests can build a two-field tree.
type OrgSnapshot = {
  units: Array<{ id: string; can_manage: boolean }>;
  members?: Array<{ user_id: string; unit_id: string | null }>;
};

/**
 * The members whose agents a department lead should see under "My team":
 * everyone assigned to a unit the viewer leads, plus the viewer themselves.
 *
 * `can_manage` on an org unit is inherited from its ancestors server-side
 * (see `orgUserLeadsUnit`), so the units it marks are exactly the lead's
 * subtree — no parent walk needed here. Returns `null` when the viewer is not
 * a department lead or is a workspace admin: an admin's "all agents" scope
 * already covers the same rows, and showing a second lens would only add a
 * menu entry that duplicates it.
 */
export function teamUserIdsFor(
  orgTree: OrgSnapshot | null | undefined,
  userId: string | null | undefined,
  isWorkspaceAdmin: boolean,
): ReadonlySet<string> | null {
  if (!orgTree || !userId || isWorkspaceAdmin) return null;
  const managedUnitIds = new Set(
    orgTree.units.filter((unit) => unit.can_manage).map((unit) => unit.id),
  );
  if (managedUnitIds.size === 0) return null;
  const ids = new Set<string>([userId]);
  for (const member of orgTree.members ?? []) {
    if (member.unit_id && managedUnitIds.has(member.unit_id)) {
      ids.add(member.user_id);
    }
  }
  return ids;
}
