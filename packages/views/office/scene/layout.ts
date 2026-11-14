/**
 * Pure geometry for the office scene: the room layout, walkability, and the
 * tile path the local avatar follows to a clicked point.
 *
 * Everything here is expressed in logical world pixels (480 x 288) or tiles
 * (16 px). The renderer draws from the same tables, so a change to the layout
 * automatically moves collisions with it.
 */

import { OFFICE_WORLD, type OfficeDir, type OfficePosition } from "@multica/core/office";

export const TILE = OFFICE_WORLD.tile;
export const COLS = OFFICE_WORLD.width / TILE;
export const ROWS = OFFICE_WORLD.height / TILE;

export interface TileRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** A desk top plus the spot its agent stands on (feet position, world px). */
export interface AgentSlot {
  desk: TileRect;
  x: number;
  y: number;
  dir: OfficeDir;
}

export interface PlantSpot {
  x: number;
  y: number;
}


export interface OfficeLayout {
  agents: AgentSlot[];
  /** Standing room for agents beyond the desk count (world px, feet). */
  overflowSpots: { x: number; y: number; dir: OfficeDir }[];
  plants: PlantSpot[];
  meetingTable: TileRect;
  sofa: TileRect;
  counter: TileRect;
  whiteboard: TileRect;
  rug: TileRect;
  /** Tile rectangles the avatar cannot walk through. */
  obstacles: TileRect[];
  /** Walkable spawn points near the entrance (world px, feet). */
  spawns: { x: number; y: number }[];
}

const DESK_COLUMNS = [12, 16, 20];
const DESK_ROWS = [4, 9];

function agentSlot(deskX: number, deskY: number): AgentSlot {
  return {
    desk: { x: deskX, y: deskY, w: 2, h: 1 },
    x: deskX * TILE + TILE,
    y: (deskY - 1) * TILE + TILE - 2,
    dir: "down",
  };
}

export function buildOfficeLayout(): OfficeLayout {
  const desks = DESK_ROWS.flatMap((row) =>
    DESK_COLUMNS.map((col) => agentSlot(col, row)),
  );

  const plants: PlantSpot[] = [
    { x: 2, y: 2 },
    { x: 26, y: 2 },
    { x: 2, y: 15 },
    { x: 26, y: 15 },
  ];
  const meetingTable: TileRect = { x: 4, y: 9, w: 4, h: 2 };
  const sofa: TileRect = { x: 2, y: 13, w: 3, h: 1 };
  const counter: TileRect = { x: 1, y: 5, w: 2, h: 1 };
  const whiteboard: TileRect = { x: 9, y: 1, w: 6, h: 1 };
  const rug: TileRect = { x: 9, y: 11, w: 12, h: 4 };

  // Agents beyond the desk count line up along the right wall.
  const overflowSpots = Array.from({ length: 6 }, (_, index) => ({
    x: 27 * TILE,
    y: (6 + index * 2) * TILE - 2,
    dir: "left" as OfficeDir,
  }));

  const obstacles: TileRect[] = [
    ...desks.map((slot) => slot.desk),
    ...plants.map((plant) => ({ x: plant.x, y: plant.y, w: 1, h: 1 })),
    meetingTable,
    sofa,
    counter,
    whiteboard,
    // Four walls: the bottom row keeps a one-tile doorway so the entrance
    // does not look sealed.
    { x: 0, y: 0, w: COLS, h: 1 },
    { x: 0, y: 0, w: 1, h: ROWS },
    { x: COLS - 1, y: 0, w: 1, h: ROWS },
    { x: 0, y: ROWS - 1, w: 13, h: 1 },
    { x: 17, y: ROWS - 1, w: COLS - 17, h: 1 },
  ];

  const spawns = [13, 15, 16].map((col) => ({
    x: col * TILE,
    y: (ROWS - 2) * TILE + TILE - 2,
  }));

  return {
    agents: desks,
    overflowSpots,
    plants,
    meetingTable,
    sofa,
    counter,
    whiteboard,
    rug,
    obstacles,
    spawns,
  };
}

export function tileAtWorld(worldX: number, worldY: number): { tx: number; ty: number } {
  return {
    tx: Math.min(Math.max(Math.floor(worldX / TILE), 0), COLS - 1),
    ty: Math.min(Math.max(Math.floor(worldY / TILE), 0), ROWS - 1),
  };
}

/** Feet position at a tile, matching the centres `findPath` returns. */
export function feetAtTile(tx: number, ty: number): { x: number; y: number } {
  return { x: tx * TILE + TILE / 2, y: ty * TILE + TILE - 2 };
}

function isObstacleTile(layout: OfficeLayout, tx: number, ty: number): boolean {
  return layout.obstacles.some(
    (rect) =>
      tx >= rect.x && tx < rect.x + rect.w && ty >= rect.y && ty < rect.y + rect.h,
  );
}

/** True when the tile exists and has no furniture or wall on it. */
export function isWalkableTile(
  layout: OfficeLayout,
  tx: number,
  ty: number,
): boolean {
  if (tx < 0 || ty < 0 || tx >= COLS || ty >= ROWS) return false;
  return !isObstacleTile(layout, tx, ty);
}

/** True when the avatar's feet can stand at this world position. */
export function isWalkableWorld(
  layout: OfficeLayout,
  worldX: number,
  worldY: number,
): boolean {
  const { tx, ty } = tileAtWorld(worldX, worldY);
  return isWalkableTile(layout, tx, ty);
}

const NEIGHBOURS = [
  { dx: 0, dy: -1 },
  { dx: 1, dy: 0 },
  { dx: 0, dy: 1 },
  { dx: -1, dy: 0 },
] as const;

/**
 * Breadth-first path over walkable tiles.
 *
 * Returns the tile centres to walk through, excluding the starting tile, or an
 * empty array when the target is blocked or unreachable. The room is 30 x 18
 * tiles, so an exhaustive BFS is cheaper than any smarter structure and never
 * needs re-planning mid-walk.
 */
export function findPath(
  layout: OfficeLayout,
  from: { x: number; y: number },
  to: { x: number; y: number },
): { x: number; y: number }[] {
  const start = tileAtWorld(from.x, from.y);
  const goal = tileAtWorld(to.x, to.y);
  if (!isWalkableTile(layout, goal.tx, goal.ty)) return [];
  if (start.tx === goal.tx && start.ty === goal.ty) return [];

  const width = COLS;
  const key = (tx: number, ty: number) => ty * width + tx;
  const cameFrom = new Map<number, number>();
  cameFrom.set(key(start.tx, start.ty), -1);
  const queue: { tx: number; ty: number }[] = [start];

  while (queue.length > 0) {
    const current = queue.shift()!;
    if (current.tx === goal.tx && current.ty === goal.ty) {
      const path: { x: number; y: number }[] = [];
      let node = key(current.tx, current.ty);
      while (node !== key(start.tx, start.ty)) {
        const tx = node % width;
        const ty = Math.floor(node / width);
        path.push(feetAtTile(tx, ty));
        node = cameFrom.get(node)!;
      }
      return path.reverse();
    }
    for (const neighbour of NEIGHBOURS) {
      const tx = current.tx + neighbour.dx;
      const ty = current.ty + neighbour.dy;
      const next = key(tx, ty);
      if (cameFrom.has(next) || !isWalkableTile(layout, tx, ty)) continue;
      cameFrom.set(next, key(current.tx, current.ty));
      queue.push({ tx, ty });
    }
  }
  return [];
}

export interface StepResult {
  x: number;
  y: number;
  dir: OfficeDir | null;
  arrived: boolean;
}

/** Moves at most `distance` world pixels toward the target. */
export function stepTowards(
  from: { x: number; y: number },
  to: { x: number; y: number },
  distance: number,
): StepResult {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const length = Math.hypot(dx, dy);
  if (length === 0 || length <= distance) {
    return { x: to.x, y: to.y, dir: directionFor(dx, dy), arrived: true };
  }
  const scale = distance / length;
  return {
    x: from.x + dx * scale,
    y: from.y + dy * scale,
    dir: directionFor(dx, dy),
    arrived: false,
  };
}

export function directionFor(dx: number, dy: number): OfficeDir | null {
  if (dx === 0 && dy === 0) return null;
  if (Math.abs(dx) >= Math.abs(dy)) return dx > 0 ? "right" : "left";
  return dy > 0 ? "down" : "up";
}

/** Stable per-member entrance spot, so a refresh does not shuffle avatars. */
export function spawnFor(layout: OfficeLayout, userId: string): OfficePosition {
  let hash = 0;
  for (let index = 0; index < userId.length; index++) {
    hash = (hash * 31 + userId.charCodeAt(index)) % 997;
  }
  const spot = layout.spawns[hash % layout.spawns.length] ?? layout.spawns[0];
  return { x: spot?.x ?? 0, y: spot?.y ?? 0, dir: "down" };
}
