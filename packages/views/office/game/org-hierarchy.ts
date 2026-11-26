/**
 * The slice of the org chart the office can actually show. Units only become
 * visible when they (or a descendant) have an agent on the floor; every
 * visible unit keeps its full ancestor chain so a level-three team still
 * reports the level-one department it belongs to.
 *
 * Pure and dependency-free so both the desk planner and the room planner can
 * share it without creating an import cycle.
 */

export interface HierUnit {
  id: string;
  name: string;
  parent_id?: string | null;
  position?: number;
}

export interface VisibleHierarchy {
  /** Visible top-level departments, in display order. */
  roots: HierUnit[];
  byId: Map<string, HierUnit>;
  /** Visible direct children, in display order. */
  childrenOf: Map<string, HierUnit[]>;
  /** Every visible unit keyed to its visible root. */
  rootOf: Map<string, string>;
  /** Full path (`研发部/研发二组`) for every visible unit. */
  pathOf: Map<string, string>;
  visibleIds: Set<string>;
}

export function compareText(left: string, right: string): number {
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

export function compareUnits(left: HierUnit, right: HierUnit): number {
  const position = (left.position ?? 0) - (right.position ?? 0);
  if (position !== 0) return position;
  const name = compareText(left.name, right.name);
  return name !== 0 ? name : compareText(left.id, right.id);
}

/**
 * Resolves the visible part of the tree.
 *
 * - Starts from units that have a direct agent and walks parents upwards.
 * - A missing parent is treated as a root.
 * - Cycles are broken deterministically: the first visible unit in display
 *   order becomes the root and the walk stops when it revisits a unit.
 */
export function visibleHierarchy(
  units: readonly HierUnit[],
  staffedUnitIds: ReadonlySet<string>,
): VisibleHierarchy {
  const byId = new Map(units.map((unit) => [unit.id, unit]));
  const visibleIds = new Set<string>();

  for (const staffedId of staffedUnitIds) {
    if (!byId.has(staffedId)) continue;
    const seen = new Set<string>();
    let currentId: string | undefined = staffedId;
    while (currentId && byId.has(currentId) && !seen.has(currentId)) {
      seen.add(currentId);
      visibleIds.add(currentId);
      const parentId: string | null | undefined = byId.get(currentId)!.parent_id;
      currentId = parentId && byId.has(parentId) ? parentId : undefined;
    }
  }

  const childrenOf = new Map<string, HierUnit[]>();
  const roots: HierUnit[] = [];
  const parentOf = new Map<string, string | null>();
  for (const unit of units) {
    if (!visibleIds.has(unit.id)) continue;
    const parentId = unit.parent_id;
    const parent = parentId && visibleIds.has(parentId) ? parentId : null;
    parentOf.set(unit.id, parent);
    if (!parent) {
      roots.push(unit);
      continue;
    }
    const siblings = childrenOf.get(parent) ?? [];
    siblings.push(unit);
    childrenOf.set(parent, siblings);
  }
  for (const siblings of childrenOf.values()) siblings.sort(compareUnits);
  roots.sort(compareUnits);

  const pathOf = new Map<string, string>();
  const rootOf = new Map<string, string>();

  const walk = (unit: HierUnit, parentPath: string, rootId: string, seen: Set<string>) => {
    if (seen.has(unit.id) || pathOf.has(unit.id)) return;
    seen.add(unit.id);
    const path = parentPath ? `${parentPath}/${unit.name}` : unit.name;
    pathOf.set(unit.id, path);
    rootOf.set(unit.id, rootId);
    for (const child of childrenOf.get(unit.id) ?? []) {
      walk(child, path, rootId, seen);
    }
  };

  for (const root of roots) walk(root, "", root.id, new Set());

  // Any visible unit left over is part of a parent cycle. Treat the first one
  // in display order as the root so the floor still gets a stable label.
  const remaining = units
    .filter((unit) => visibleIds.has(unit.id) && !pathOf.has(unit.id))
    .sort(compareUnits);
  for (const unit of remaining) {
    if (pathOf.has(unit.id)) continue;
    roots.push(unit);
    walk(unit, "", unit.id, new Set());
  }
  roots.sort(compareUnits);

  return { roots, byId, childrenOf, rootOf, pathOf, visibleIds };
}
