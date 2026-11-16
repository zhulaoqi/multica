import type { OrgTree } from "@multica/core/api";

type Unit = OrgTree["units"][number];

export interface DepartmentNode<T> {
  /** Unit id, or "" for owners placed in no visible department. */
  unitId: string;
  name: string;
  leadUserId: string;
  /** Rows whose owner sits directly in this department. */
  rows: T[];
  children: DepartmentNode<T>[];
}

/**
 * Lays rows out as the org tree: each department that holds an owner of some
 * row, or sits above one, becomes a node carrying its direct rows. Siblings
 * keep their org-chart position. Departments with nothing beneath them are
 * dropped so the tree only shows the team. Owners outside every visible
 * department close the list as a root with unitId "".
 */
export function departmentForest<T>(
  rows: readonly T[],
  ownerOf: (row: T) => string | null | undefined,
  tree: Pick<OrgTree, "units" | "members">,
): DepartmentNode<T>[] {
  const unitIds = new Set(tree.units.map((unit) => unit.id));
  const unitByUser = new Map(
    tree.members
      .filter((member) => member.unit_id && unitIds.has(member.unit_id))
      .map((member) => [member.user_id, member.unit_id]),
  );
  const direct = new Map<string, T[]>();
  for (const row of rows) {
    const owner = ownerOf(row);
    const unitId = (owner && unitByUser.get(owner)) || "";
    const bucket = direct.get(unitId);
    if (bucket) bucket.push(row);
    else direct.set(unitId, [row]);
  }

  const children = new Map<string, Unit[]>();
  const roots: Unit[] = [];
  for (const unit of tree.units) {
    if (unit.parent_id && unitIds.has(unit.parent_id)) {
      const list = children.get(unit.parent_id);
      if (list) list.push(unit);
      else children.set(unit.parent_id, [unit]);
    } else {
      roots.push(unit);
    }
  }
  const ordered = (units: Unit[]) =>
    [...units].sort((a, b) => (a.position ?? 0) - (b.position ?? 0) || a.name.localeCompare(b.name));

  const seen = new Set<string>();
  const build = (unit: Unit): DepartmentNode<T> | null => {
    if (seen.has(unit.id)) return null;
    seen.add(unit.id);
    const kids = ordered(children.get(unit.id) ?? [])
      .map(build)
      .filter((node): node is DepartmentNode<T> => node !== null);
    const own = direct.get(unit.id) ?? [];
    if (own.length === 0 && kids.length === 0) return null;
    return { unitId: unit.id, name: unit.name, leadUserId: unit.lead_user_id ?? "", rows: own, children: kids };
  };
  const forest = ordered(roots)
    .map(build)
    .filter((node): node is DepartmentNode<T> => node !== null);

  const loose = direct.get("");
  if (loose?.length) forest.push({ unitId: "", name: "", leadUserId: "", rows: loose, children: [] });
  return forest;
}

/** Rows anywhere under a node, for the department card's count. */
export function countDepartmentRows<T>(node: DepartmentNode<T>): number {
  return node.children.reduce((sum, child) => sum + countDepartmentRows(child), node.rows.length);
}
