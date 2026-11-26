/**
 * Departments borrow a room and grow with the work their agents land. The
 * mapping is cosmetic: every room stays walkable and every station stays
 * usable regardless of which department is displayed there.
 */

import type { RoomId } from "../scene/layout";
import { compareText, visibleHierarchy } from "./org-hierarchy";

/** Rooms that can host a department plaque, in assignment order. */
export const DEPARTMENT_ROOMS: readonly RoomId[] = [
  "office",
  "meeting",
  "server",
  "print",
  "lounge",
  "kitchen",
];

export interface OrgUnitLike {
  id: string;
  name: string;
  parent_id?: string | null;
  position?: number;
}

export interface OrgAgentLike {
  id: string;
  unit_id?: string;
}

/** Completions needed before a department room gains a level. */
export const DEPARTMENT_COMPLETIONS_PER_LEVEL = 10;
export const MAX_DEPARTMENT_LEVEL = 9;

/** Level a department has reached after `completed` finished tasks. */
export function departmentLevel(completed: number): number {
  return Math.min(
    MAX_DEPARTMENT_LEVEL,
    Math.floor(Math.max(0, completed) / DEPARTMENT_COMPLETIONS_PER_LEVEL) + 1,
  );
}

export interface DepartmentRoom {
  room: RoomId;
  unitId: string;
  name: string;
  completed: number;
  level: number;
  /** Filled stars, capped at five so the plaque stays readable. */
  stars: number;
  agentCount: number;
}

/**
 * One room per department, busiest first. Departments without a visible agent
 * are skipped so the plaques reflect actual staffing. Ties break on name so
 * the rooms do not reshuffle between renders.
 */
export function departmentRooms(
  units: readonly OrgUnitLike[],
  agents: readonly OrgAgentLike[],
  completedByAgent: ReadonlyMap<string, number>,
  rooms: readonly RoomId[] = DEPARTMENT_ROOMS,
): DepartmentRoom[] {
  const byUnit = new Map<
    string,
    { agents: number; completed: number }
  >();
  for (const agent of agents) {
    const unitId = agent.unit_id ?? "";
    if (!unitId) continue;
    const entry = byUnit.get(unitId) ?? { agents: 0, completed: 0 };
    entry.agents += 1;
    entry.completed += Math.max(0, completedByAgent.get(agent.id) ?? 0);
    byUnit.set(unitId, entry);
  }

  const hierarchy = visibleHierarchy(units, new Set(byUnit.keys()));
  const names = new Map(units.map((unit) => [unit.id, unit.name]));
  // A room belongs to a visible top-level department; sub-department work
  // rolls up into it so the parent plaque reflects the whole subtree.
  const byRoot = new Map<string, { agents: number; completed: number }>();
  for (const [unitId, entry] of byUnit) {
    const rootId = hierarchy.rootOf.get(unitId) ?? unitId;
    const total = byRoot.get(rootId) ?? { agents: 0, completed: 0 };
    total.agents += entry.agents;
    total.completed += entry.completed;
    byRoot.set(rootId, total);
  }

  const ranked = [...byRoot.entries()]
    .filter(([rootId, entry]) => entry.agents > 0 && names.has(rootId))
    .sort(([leftId, left], [rightId, right]) => {
      if (right.completed !== left.completed) return right.completed - left.completed;
      const byName = compareText(names.get(leftId) ?? "", names.get(rightId) ?? "");
      return byName !== 0 ? byName : compareText(leftId, rightId);
    })
    .slice(0, rooms.length);

  return ranked.map(([unitId, entry], index) => {
    const level = departmentLevel(entry.completed);
    return {
      room: rooms[index]!,
      unitId,
      name: names.get(unitId) ?? "",
      completed: entry.completed,
      level,
      stars: Math.min(5, level),
      agentCount: entry.agents,
    };
  });
}
