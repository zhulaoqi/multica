/**
 * Walk-up interaction geometry: where a visitor can stand next to an agent,
 * and which agent is close enough to talk to.
 *
 * Everything here is pure so the scene can call it every frame without
 * allocating React state. Positions are logical world pixels (feet position),
 * matching the rest of the office scene.
 */

import type { OfficeDir } from "@multica/core/office";
import {
  COLS,
  ROWS,
  TILE,
  feetAtTile,
  findPath,
  isWalkableTile,
  tileAtWorld,
  type OfficeLayout,
} from "./layout";

export interface InteractableAgent {
  id: string;
  /** Feet position, the same space as `OfficePosition`. */
  x: number;
  y: number;
}

export interface VisitSlot {
  x: number;
  y: number;
  /** Direction the visitor faces once standing on the slot. */
  dir: OfficeDir;
}

/**
 * Side positions first: desks sit below their agent, so the tile in front is
 * usually furniture. The visit slot only has to be walkable; the caller falls
 * back to opening the panel directly when none is reachable.
 */
const SLOT_OFFSETS = [
  { dx: -1, dy: 0, dir: "right" },
  { dx: 1, dy: 0, dir: "left" },
  { dx: 0, dy: 1, dir: "up" },
  { dx: 0, dy: -1, dir: "down" },
] as const;

/** Walkable spots around an agent, deduplicated by tile. */
export function visitSlots(
  layout: OfficeLayout,
  agent: { x: number; y: number },
): VisitSlot[] {
  const { tx, ty } = tileAtWorld(agent.x, agent.y);
  const seen = new Set<string>();
  const slots: VisitSlot[] = [];

  for (const offset of SLOT_OFFSETS) {
    const slotX = tx + offset.dx;
    const slotY = ty + offset.dy;
    if (slotX < 0 || slotY < 0 || slotX >= COLS || slotY >= ROWS) continue;
    if (!isWalkableTile(layout, slotX, slotY)) continue;
    const key = `${slotX}:${slotY}`;
    if (seen.has(key)) continue;
    seen.add(key);
    slots.push({ ...feetAtTile(slotX, slotY), dir: offset.dir });
  }

  return slots;
}

/** Nearest agent within `radius` world pixels, or null. */
export function nearestAgent(
  agents: readonly InteractableAgent[],
  point: { x: number; y: number },
  radius: number,
): InteractableAgent | null {
  let best: InteractableAgent | null = null;
  let bestDistance = radius;

  for (const agent of agents) {
    const distance = Math.hypot(agent.x - point.x, agent.y - point.y);
    if (distance <= bestDistance) {
      best = agent;
      bestDistance = distance;
    }
  }

  return best;
}

/**
 * The reachable visit slot closest to `from`. Returns null when the agent has
 * no walkable neighbour or none of them can be reached, so the caller can open
 * the panel without walking instead of getting stuck.
 */
export function nearestVisitSlot(
  layout: OfficeLayout,
  agent: { x: number; y: number },
  from: { x: number; y: number },
): VisitSlot | null {
  let best: VisitSlot | null = null;
  let bestCost = Number.POSITIVE_INFINITY;

  for (const slot of visitSlots(layout, agent)) {
    const path = findPath(layout, from, slot);
    const direct = Math.hypot(slot.x - from.x, slot.y - from.y);
    const reachable = path.length > 0 || direct < TILE;
    if (!reachable) continue;
    // Path length dominates; the direct distance breaks ties between slots
    // that need the same number of steps.
    const cost = path.length * TILE + direct;
    if (cost < bestCost) {
      best = slot;
      bestCost = cost;
    }
  }

  return best;
}
