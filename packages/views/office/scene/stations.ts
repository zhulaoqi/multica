/**
 * Places an agent character can stand or sit, expressed in world pixels.
 *
 * Agents pick a station from their runtime status: a working agent types at
 * its own desk, a queued agent joins the meeting table (the visible
 * "collaboration" pose), and an idle agent drifts between its desk and the
 * room's shared spots. Every station is a walkable feet position so the tile
 * path always finds a route.
 */

import type { OfficeDir } from "@multica/core/office";
import { TILE, type AgentSlot, type OfficeLayout } from "./layout";

export type StationKind = "desk" | "meeting" | "leisure";

export interface Station {
  /** Stable identity so a routine can tell when it already targets it. */
  key: string;
  x: number;
  y: number;
  /** Facing once the agent arrives. */
  dir: OfficeDir;
  kind: StationKind;
  /** Seated poses (typing, reading, sleeping) apply at desks and the table. */
  seat: boolean;
}

/** The agent's own chair, in front of the desk monitor. */
export function deskStation(slot: AgentSlot, index: number): Station {
  return {
    key: `desk:${index}`,
    x: slot.x,
    y: slot.y,
    dir: "down",
    kind: "desk",
    seat: true,
  };
}

/** Seats around the meeting table; two on the far side, two on the near. */
export function meetingStations(layout: OfficeLayout): Station[] {
  const table = layout.meetingTable;
  const top = table.y * TILE - 2;
  const bottom = (table.y + table.h) * TILE + 14;
  const left = (table.x + 0.5) * TILE;
  const right = (table.x + 2.5) * TILE;
  return [
    { key: "meeting:0", x: left, y: top, dir: "down", kind: "meeting", seat: true },
    { key: "meeting:1", x: right, y: top, dir: "down", kind: "meeting", seat: true },
    { key: "meeting:2", x: left, y: bottom, dir: "up", kind: "meeting", seat: true },
    { key: "meeting:3", x: right, y: bottom, dir: "up", kind: "meeting", seat: true },
  ];
}

/** Shared spots for idle agents: sofa, coffee counter, board, windows. */
export function leisureStations(layout: OfficeLayout): Station[] {
  const sofa = layout.sofa;
  const counter = layout.counter;
  const board = layout.whiteboard;
  return [
    {
      key: "leisure:sofa",
      x: (sofa.x + 1) * TILE,
      y: (sofa.y + sofa.h) * TILE + 14,
      dir: "up",
      kind: "leisure",
      seat: false,
    },
    {
      key: "leisure:counter",
      x: (counter.x + 1) * TILE,
      y: (counter.y + counter.h) * TILE + 14,
      dir: "up",
      kind: "leisure",
      seat: false,
    },
    {
      key: "leisure:board",
      x: (board.x + 2) * TILE,
      y: (board.y + 1) * TILE + 14,
      dir: "up",
      kind: "leisure",
      seat: false,
    },
    ...layout.plants.map((plant, index) => ({
      key: `leisure:plant:${index}`,
      x: plant.x * TILE + TILE / 2,
      y: (plant.y + 1) * TILE + 14,
      dir: "up" as OfficeDir,
      kind: "leisure" as StationKind,
      seat: false,
    })),
  ];
}

/**
 * The idle agent's next destination. Idle agents alternate between their desk
 * and the shared spot the room assigned them, which reads as "drifted off for
 * coffee and came back" without any randomness at render time.
 */
export function idleErrandAt(
  step: number,
  home: Station,
  errand: Station | null,
): Station {
  if (step % 2 === 0 || !errand) return home;
  return errand;
}

/** Rotates a station list so agents standing together never share a spot. */
export function assignStations(seats: readonly Station[], index: number): Station | null {
  if (seats.length === 0) return null;
  return seats[index % seats.length] ?? null;
}
