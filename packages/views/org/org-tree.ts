import type { OrgAgent, OrgMember, OrgTree, OrgUnit } from "@multica/core/api";

export interface OrgNode {
  unit: OrgUnit;
  agents: OrgAgent[];
  members: OrgMember[];
  children: OrgNode[];
}

export interface OrgForest {
  roots: OrgNode[];
  unassignedAgents: OrgAgent[];
  unassignedMembers: OrgMember[];
}

/** Resolves the full path (研发中心/支付组) for every unit. */
export function unitPaths(units: OrgUnit[]): Map<string, string> {
  const byID = new Map(units.map((unit) => [unit.id, unit]));
  const paths = new Map<string, string>();
  const resolve = (unit: OrgUnit, seen: Set<string>): string => {
    const known = paths.get(unit.id);
    if (known) return known;
    if (seen.has(unit.id)) return unit.name;
    seen.add(unit.id);
    const parent = unit.parent_id ? byID.get(unit.parent_id) : undefined;
    const path = parent ? `${resolve(parent, seen)}/${unit.name}` : unit.name;
    paths.set(unit.id, path);
    return path;
  };
  for (const unit of units) resolve(unit, new Set());
  return paths;
}

/** Groups agents and members under their unit and collects the unassigned. */
export function buildOrgForest(tree: Pick<OrgTree, "units" | "agents" | "members">): OrgForest {
  const nodes = new Map<string, OrgNode>();
  for (const unit of tree.units) {
    nodes.set(unit.id, {
      unit,
      agents: tree.agents.filter((agent) => agent.unit_id === unit.id),
      members: tree.members.filter((member) => member.unit_id === unit.id),
      children: [],
    });
  }
  const roots: OrgNode[] = [];
  for (const node of nodes.values()) {
    const parent = node.unit.parent_id ? nodes.get(node.unit.parent_id) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  return {
    roots,
    unassignedAgents: tree.agents.filter((agent) => !agent.unit_id || !nodes.has(agent.unit_id)),
    unassignedMembers: tree.members.filter((member) => !member.unit_id || !nodes.has(member.unit_id)),
  };
}
