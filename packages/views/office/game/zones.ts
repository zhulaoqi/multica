/**
 * Seats the office by department so each staffed top-level department owns a
 * contiguous desk cluster and its sub-departments stay visually nested under
 * it. Pure and deterministic: agents keep their seats while status and
 * activity change; only org-chart or roster edits move them.
 */

import type { DeskZoneSpec } from "../scene/layout";
import { OFFICE_DESK_COUNT, OFFICE_DESK_ZONES } from "../scene/layout";
import { departmentLevel } from "./departments";
import {
  compareText,
  compareUnits,
  visibleHierarchy,
  type HierUnit,
} from "./org-hierarchy";

export interface DeskZone {
  key: string;
  /** The visible top-level department this cluster belongs to. */
  unitId: string;
  /** Top-level department name. */
  label: string;
  /** `研发部 › AIHUB`, or `研发部 › AIHUB、BI中台` when a cluster is shared. */
  pathLabel: string;
  level: number;
  anchor: { tx: number; ty: number };
  /** Agents seated in this cluster, including those without a known unit. */
  agentCount: number;
  /** Visible direct sub-department names represented in this cluster. */
  children: string[];
}

export type ZoneUnit = HierUnit;

export interface ZoneAgent {
  id: string;
}

export interface ZoneSeat {
  id: string;
  /** Home slot: desk indexes come first, then overflow standing spots. */
  home: number;
}

export interface ZoneAssignment {
  /** Seated agents sorted by home slot. */
  seats: ZoneSeat[];
  /** One plaque per cluster that has a known department in it. */
  zones: DeskZone[];
  /** Agents beyond the available home slots; the scene cannot render them. */
  unseated: string[];
}

interface ZoneGroup {
  rootId: string;
  unitId: string;
  label: string;
  isRootGroup: boolean;
  agentIds: string[];
  order: HierUnit;
}

/**
 * Orders a department's agents by id and drops duplicates, so the same roster
 * always produces the same seating.
 */
function rosterFor(ids: readonly string[]): string[] {
  return [...new Set(ids)].sort(compareText);
}

function groupFor(
  root: HierUnit,
  unit: HierUnit,
  agentIds: string[],
  isRootGroup: boolean,
): ZoneGroup {
  return {
    rootId: root.id,
    unitId: unit.id,
    label: unit.name,
    isRootGroup,
    agentIds,
    order: unit,
  };
}

export function assignDeskZones(
  agents: readonly ZoneAgent[],
  unitByAgent: ReadonlyMap<string, string>,
  units: readonly ZoneUnit[],
  completedByAgent: ReadonlyMap<string, number>,
  specs: readonly DeskZoneSpec[] = OFFICE_DESK_ZONES,
): ZoneAssignment {
  const seatOrder: number[] = [];
  const seenHomes = new Set<number>();
  for (const spec of specs) {
    for (const home of spec.homes) {
      if (seenHomes.has(home)) continue;
      seenHomes.add(home);
      seatOrder.push(home);
    }
  }

  const seenAgents = new Set<string>();
  const orderedAgents = agents.filter((agent) => {
    if (seenAgents.has(agent.id)) return false;
    seenAgents.add(agent.id);
    return true;
  });

  const unitById = new Map(units.map((unit) => [unit.id, unit]));
  const byUnit = new Map<string, string[]>();
  const loose: string[] = [];
  for (const agent of orderedAgents) {
    const unitId = unitByAgent.get(agent.id);
    if (!unitId || !unitById.has(unitId)) {
      loose.push(agent.id);
      continue;
    }
    const bucket = byUnit.get(unitId);
    if (bucket) bucket.push(agent.id);
    else byUnit.set(unitId, [agent.id]);
  }

  // No department data: keep the historical behaviour of seating in input
  // order, so a workspace without an org chart looks exactly as it did.
  if (byUnit.size === 0) {
    const identityHomes = [...seatOrder].sort((a, b) => a - b);
    const seats: ZoneSeat[] = [];
    const unseated: string[] = [];
    orderedAgents.forEach((agent, index) => {
      const home = identityHomes[index];
      if (home === undefined) unseated.push(agent.id);
      else seats.push({ id: agent.id, home });
    });
    return { seats, zones: [], unseated };
  }

  const hierarchy = visibleHierarchy(units, new Set(byUnit.keys()));
  const roots = hierarchy.roots;

  // A group is the unit whose name appears on the plaque: the root's direct
  // employees, or everything below one of its visible direct children.
  const groupForUnit = (root: HierUnit, child: HierUnit): ZoneGroup => {
    const ids: string[] = [];
    const seen = new Set<string>();
    const walk = (unitId: string) => {
      if (seen.has(unitId)) return;
      seen.add(unitId);
      ids.push(...(byUnit.get(unitId) ?? []));
      for (const nested of hierarchy.childrenOf.get(unitId) ?? []) walk(nested.id);
    };
    walk(child.id);
    return groupFor(root, child, rosterFor(ids), false);
  };

  const groupsByRoot = new Map<string, ZoneGroup[]>();
  for (const root of roots) {
    const groups: ZoneGroup[] = [];
    const direct = rosterFor(byUnit.get(root.id) ?? []);
    if (direct.length > 0) groups.push(groupFor(root, root, direct, true));
    for (const child of hierarchy.childrenOf.get(root.id) ?? []) {
      const group = groupForUnit(root, child);
      if (group.agentIds.length > 0) groups.push(group);
    }
    groups.sort((left, right) => {
      if (right.agentIds.length !== left.agentIds.length) {
        return right.agentIds.length - left.agentIds.length;
      }
      return compareUnits(left.order, right.order);
    });
    groupsByRoot.set(root.id, groups);
  }

  // Each root gets at least one cluster; extra clusters rotate through the
  // roots in display order. With more roots than clusters, the leading roots
  // keep a cluster and the rest spill into spare seats.
  const clustersByRoot = new Map<string, DeskZoneSpec[]>();
  const rootByCluster = new Map<string, string>();
  for (const root of roots) clustersByRoot.set(root.id, []);
  if (roots.length > 0) {
    roots.forEach((root, index) => {
      let mine: DeskZoneSpec[];
      if (roots.length <= specs.length) {
        mine = specs.filter((_, specIndex) => specIndex % roots.length === index);
      } else {
        mine = index < specs.length ? [specs[index]!] : [];
      }
      clustersByRoot.set(root.id, mine);
      for (const spec of mine) rootByCluster.set(spec.key, root.id);
    });
  }

  const seats: ZoneSeat[] = [];
  const used = new Set<number>();
  const spill: string[] = [];
  const groupLabelByAgent = new Map<string, string>();
  const rootBySeat = new Map<string, string>();

  for (const root of roots) {
    const clusters = clustersByRoot.get(root.id) ?? [];
    const groups = groupsByRoot.get(root.id) ?? [];
    if (clusters.length === 0) {
      for (const group of groups) spill.push(...group.agentIds);
      continue;
    }
    groups.forEach((group, groupIndex) => {
      const preferredIndex = Math.min(groupIndex, clusters.length - 1);
      const preferred = clusters[preferredIndex]!;
      const searchOrder = [
        preferred,
        ...clusters.filter((cluster) => cluster !== preferred),
      ];
      for (const agentId of group.agentIds) {
        let home: number | undefined;
        for (const spec of searchOrder) {
          const free = spec.homes.find((candidate) => !used.has(candidate));
          if (free !== undefined) {
            home = free;
            break;
          }
        }
        if (home === undefined) {
          spill.push(agentId);
          continue;
        }
        used.add(home);
        seats.push({ id: agentId, home });
        rootBySeat.set(agentId, root.id);
        if (!group.isRootGroup) groupLabelByAgent.set(agentId, group.label);
      }
    });
  }

  // Spilled agents fill spare desks first (left to right, row by row), then
  // the extra standing spots. Loose agents without a known unit go last.
  const spareDesks = seatOrder
    .filter((home) => home < OFFICE_DESK_COUNT && !used.has(home))
    .sort((a, b) => a - b);
  const spareSpots = seatOrder
    .filter((home) => home >= OFFICE_DESK_COUNT && !used.has(home))
    .sort((a, b) => a - b);
  const spare = [...spareDesks, ...spareSpots];
  const unseated: string[] = [];
  const spillRoster = [...spill, ...rosterFor(loose)];
  spillRoster.forEach((agentId, index) => {
    const home = spare[index];
    if (home === undefined) {
      unseated.push(agentId);
      return;
    }
    used.add(home);
    seats.push({ id: agentId, home });
  });

  const completedByRoot = new Map<string, number>();
  for (const agent of orderedAgents) {
    const unitId = unitByAgent.get(agent.id);
    if (!unitId || !unitById.has(unitId)) continue;
    const rootId = hierarchy.rootOf.get(unitId);
    if (!rootId) continue;
    completedByRoot.set(
      rootId,
      (completedByRoot.get(rootId) ?? 0) +
        Math.max(0, completedByAgent.get(agent.id) ?? 0),
    );
  }

  seats.sort((left, right) => left.home - right.home);

  const zones: DeskZone[] = [];
  for (const spec of specs) {
    const occupants = seats.filter((seat) => spec.homes.includes(seat.home));
    if (occupants.length === 0) continue;
    const rootId = rootByCluster.get(spec.key);
    if (!rootId) continue;
    // Loose agents and cross-root spill may land in a cluster, but only
    // agents from the cluster's own department tree justify its plaque.
    if (!occupants.some((seat) => rootBySeat.get(seat.id) === rootId)) continue;
    const root = hierarchy.byId.get(rootId);
    if (!root) continue;
    // Children are ordered by their first seat in the cluster, so sharing a
    // cluster keeps the plaque text stable.
    const children: string[] = [];
    for (const seat of occupants) {
      const child = groupLabelByAgent.get(seat.id);
      if (child && !children.includes(child)) children.push(child);
    }
    const pathLabel = children.length
      ? `${root.name} › ${children.join("、")}`
      : root.name;
    zones.push({
      key: spec.key,
      unitId: root.id,
      label: root.name,
      pathLabel,
      level: departmentLevel(completedByRoot.get(root.id) ?? 0),
      anchor: spec.anchor,
      agentCount: occupants.length,
      children,
    });
  }

  return { seats, zones, unseated };
}
