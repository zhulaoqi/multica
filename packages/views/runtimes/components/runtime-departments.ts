import type { OrgTree } from "@multica/core/api";
import { unitPaths } from "../../org/org-tree";
import type { RuntimeMachine } from "./runtime-machines";

export interface DepartmentMachines {
  /** Unit id, or "" for owners placed in no department the viewer can see. */
  unitId: string;
  /** Full path such as 研发部/开发二组; empty for the unassigned bucket. */
  path: string;
  machines: RuntimeMachine[];
}

/**
 * Buckets machines by their owner's department. The viewer's own department
 * leads, the rest follow by path, and owners outside any visible department
 * close the list. Returns null when the viewer sees only their own machines,
 * so the page keeps its single "Machines" band for them.
 */
export function groupMachinesByDepartment(
  machines: readonly RuntimeMachine[],
  tree: Pick<OrgTree, "units" | "members" | "my_unit_id">,
  viewerId: string | null | undefined,
): DepartmentMachines[] | null {
  const ownerOf = (machine: RuntimeMachine) => machine.runtimes[0]?.owner_id ?? "";
  if (machines.every((machine) => ownerOf(machine) === viewerId)) return null;

  const paths = unitPaths(tree.units);
  const unitByUser = new Map(
    tree.members
      .filter((member) => member.unit_id && paths.has(member.unit_id))
      .map((member) => [member.user_id, member.unit_id]),
  );

  const buckets = new Map<string, RuntimeMachine[]>();
  for (const machine of machines) {
    const ownerId = ownerOf(machine);
    const unitId = (ownerId && unitByUser.get(ownerId)) || "";
    const bucket = buckets.get(unitId);
    if (bucket) bucket.push(machine);
    else buckets.set(unitId, [machine]);
  }
  if (buckets.size === 1 && buckets.has("")) return null;

  const rank = (unitId: string) => {
    if (unitId === "") return 2;
    return unitId === tree.my_unit_id ? 0 : 1;
  };
  return [...buckets.entries()]
    .map(([unitId, grouped]) => ({
      unitId,
      path: unitId ? (paths.get(unitId) ?? "") : "",
      machines: grouped,
    }))
    .sort((a, b) => rank(a.unitId) - rank(b.unitId) || a.path.localeCompare(b.path));
}
