/**
 * Seats the office by department so each staffed unit owns a contiguous desk
 * cluster. Pure and deterministic: agents keep their seats while status and
 * activity change; only org-chart or roster edits move them.
 */

import type { DeskZoneSpec } from "../scene/layout";
import { OFFICE_DESK_COUNT, OFFICE_DESK_ZONES } from "../scene/layout";
import { departmentLevel } from "./departments";

export interface DeskZone {
  key: string;
  unitId: string;
  label: string;
  level: number;
  anchor: { tx: number; ty: number };
  /** Agents seated in this cluster, including those without a known unit. */
  agentCount: number;
}

export interface ZoneUnit {
  id: string;
  name: string;
  position?: number;
}

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

function compareText(left: string, right: string): number {
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

/**
 * Orders a department's agents by id and drops duplicates, so the same roster
 * always produces the same seating.
 */
function rosterFor(ids: readonly string[]): string[] {
  return [...new Set(ids)].sort(compareText);
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

  const orderedUnits = [...byUnit.keys()]
    .map((unitId) => unitById.get(unitId)!)
    .sort((left, right) => {
      const position = (left.position ?? 0) - (right.position ?? 0);
      if (position !== 0) return position;
      const name = compareText(left.name, right.name);
      return name !== 0 ? name : compareText(left.id, right.id);
    })
    .map((unit) => unit.id);

  const seats: ZoneSeat[] = [];
  const used = new Set<number>();
  const spill: string[] = [];

  orderedUnits.forEach((unitId, specIndex) => {
    const roster = rosterFor(byUnit.get(unitId) ?? []);
    const spec = specs[specIndex];
    if (!spec) {
      spill.push(...roster);
      return;
    }
    spec.homes.forEach((home, index) => {
      const agentId = roster[index];
      if (agentId === undefined) return;
      used.add(home);
      seats.push({ id: agentId, home });
    });
    for (const agentId of roster.slice(spec.homes.length)) spill.push(agentId);
  });
  spill.push(...rosterFor(loose));

  // Spilled agents fill spare desks first (left to right, row by row), then
  // the extra standing spots.
  const spareDesks = seatOrder
    .filter((home) => home < OFFICE_DESK_COUNT && !used.has(home))
    .sort((a, b) => a - b);
  const spareSpots = seatOrder
    .filter((home) => home >= OFFICE_DESK_COUNT && !used.has(home))
    .sort((a, b) => a - b);
  const spare = [...spareDesks, ...spareSpots];
  const unseated: string[] = [];
  spill.forEach((agentId, index) => {
    const home = spare[index];
    if (home === undefined) {
      unseated.push(agentId);
      return;
    }
    used.add(home);
    seats.push({ id: agentId, home });
  });

  const completedByUnit = new Map<string, number>();
  for (const agent of orderedAgents) {
    const unitId = unitByAgent.get(agent.id);
    if (!unitId || !unitById.has(unitId)) continue;
    completedByUnit.set(
      unitId,
      (completedByUnit.get(unitId) ?? 0) +
        Math.max(0, completedByAgent.get(agent.id) ?? 0),
    );
  }

  seats.sort((left, right) => left.home - right.home);

  const zones: DeskZone[] = [];
  for (const spec of specs) {
    const occupants = seats.filter((seat) => spec.homes.includes(seat.home));
    if (occupants.length === 0) continue;
    const counts = new Map<string, number>();
    let dominant: string | null = null;
    let dominantCount = 0;
    for (const seat of occupants) {
      const unitId = unitByAgent.get(seat.id);
      if (!unitId || !unitById.has(unitId)) continue;
      const count = (counts.get(unitId) ?? 0) + 1;
      counts.set(unitId, count);
      if (count > dominantCount) {
        dominant = unitId;
        dominantCount = count;
      }
    }
    if (!dominant) continue;
    const unit = unitById.get(dominant)!;
    zones.push({
      key: spec.key,
      unitId: dominant,
      label: unit.name,
      level: departmentLevel(completedByUnit.get(dominant) ?? 0),
      anchor: spec.anchor,
      agentCount: occupants.length,
    });
  }

  return { seats, zones, unseated };
}
