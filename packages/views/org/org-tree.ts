import type { OrgAgent, OrgMember, OrgSquad, OrgTree, OrgUnit } from "@multica/core/api";

export interface OrgNode {
  unit: OrgUnit;
  agents: OrgAgent[];
  members: OrgMember[];
  squads: OrgSquad[];
  children: OrgNode[];
}

export interface OrgForest {
  roots: OrgNode[];
  unassignedAgents: OrgAgent[];
  unassignedMembers: OrgMember[];
  unassignedSquads: OrgSquad[];
}

/** The agent a member registered as their own digital twin, if any. */
export function memberTwin(agents: OrgAgent[], userID: string): OrgAgent | null {
  if (!userID) return null;
  return agents.find((agent) => agent.owner_id === userID) ?? null;
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

/** The unit and every department below it, so a department's squad covers
 * agents placed in sub-departments too. */
export function unitSubtreeIDs(units: OrgUnit[], rootId: string): Set<string> {
  const children = new Map<string, string[]>();
  for (const unit of units) {
    if (!unit.parent_id) continue;
    const siblings = children.get(unit.parent_id) ?? [];
    siblings.push(unit.id);
    children.set(unit.parent_id, siblings);
  }
  const ids = new Set<string>();
  const walk = (id: string) => {
    if (!id || ids.has(id)) return;
    ids.add(id);
    for (const child of children.get(id) ?? []) walk(child);
  };
  walk(rootId);
  return ids;
}

/** Groups agents, members and squads under their unit and collects the rest. */
export function buildOrgForest(
  tree: Pick<OrgTree, "units" | "agents" | "members"> & { squads?: OrgSquad[] },
): OrgForest {
  const squads = tree.squads ?? [];
  const nodes = new Map<string, OrgNode>();
  for (const unit of tree.units) {
    nodes.set(unit.id, {
      unit,
      agents: tree.agents.filter((agent) => agent.unit_id === unit.id),
      members: tree.members.filter((member) => member.unit_id === unit.id),
      squads: squads.filter((squad) => squad.unit_id === unit.id),
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
    unassignedSquads: squads.filter((squad) => !squad.unit_id || !nodes.has(squad.unit_id)),
  };
}

/** Whether a squad is busy, waiting for scheduling, or has no agents at all. */
export type SquadAvailability = "working" | "waiting" | "empty";

export function squadAvailability(squad: Pick<OrgSquad, "agent_count" | "working_agents">): SquadAvailability {
  if (squad.working_agents > 0) return "working";
  if (squad.agent_count > 0) return "waiting";
  return "empty";
}

/** Availability bucket for an agent's runtime status. */
export type AgentAvailability = "working" | "waiting" | "attention" | "offline" | "unknown";

export function agentAvailability(status: string): AgentAvailability {
  switch (status) {
    case "working":
      return "working";
    case "idle":
      return "waiting";
    case "blocked":
    case "error":
      return "attention";
    case "offline":
      return "offline";
    default:
      return "unknown";
  }
}
