/**
 * Pure geometry for the office scene: the floor plan, walkability, and the
 * tile path a character follows to reach a station.
 *
 * Everything here is expressed in logical world pixels (896 x 544) or tiles
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

export type RoomId =
  | "meeting"
  | "print"
  | "kitchen"
  | "office"
  | "lounge"
  | "reception"
  | "server";

export interface Room {
  id: RoomId;
  rect: TileRect;
}

export type FurnitureKind =
  | "desk"
  | "chair"
  | "stool"
  | "vending"
  | "meeting-table"
  | "whiteboard"
  | "screen"
  | "counter"
  | "coffee-machine"
  | "water-cooler"
  | "fridge"
  | "kitchen-table"
  | "printer"
  | "rack"
  | "sofa"
  | "beanbag"
  | "coffee-table"
  | "bookshelf"
  | "reception-desk"
  | "plant"
  | "lamp"
  | "clock"
  | "aquarium"
  | "poster"
  | "window";

export interface Furniture {
  id: string;
  kind: FurnitureKind;
  /** Tile rectangle the item occupies. */
  rect: TileRect;
  /** Whether characters collide with it. Chairs and wall decor do not. */
  blocks: boolean;
  /** Direction a character faces when using the item, when relevant. */
  face?: OfficeDir;
}

export type StationKind =
  | "desk"
  | "meeting"
  | "coffee"
  | "water"
  | "snack"
  | "printer"
  | "server"
  | "sofa"
  | "beanbag"
  | "kitchen-table"
  | "reception"
  | "plant"
  | "door";

/** A spot an agent stands or sits on, in world pixels (feet position). */
export interface Station {
  /** Stable identity across rebuilds, used for reservations and routing. */
  key: string;
  x: number;
  y: number;
  /** Facing once the character arrives. */
  dir: OfficeDir;
  kind: StationKind;
  /** Seated poses (typing, reading) apply while standing here. */
  seat: boolean;
  /** Agents allowed on the station at the same time. */
  capacity: number;
}

export interface OfficeLayout {
  agents: AgentSlot[];
  /** Standing room for agents beyond the desk count (world px, feet). */
  overflowSpots: { x: number; y: number; dir: OfficeDir }[];
  rooms: Room[];
  furniture: Furniture[];
  /** Wall blocks, kept separately so the renderer can draw their front faces. */
  walls: TileRect[];
  /** Gaps in the walls, so the renderer can draw thresholds and posts. */
  doors: TileRect[];
  stations: Station[];
  /** Tile rectangles the avatar cannot walk through. */
  obstacles: TileRect[];
  /** Walkable spawn points near the entrance (world px, feet). */
  spawns: { x: number; y: number }[];
  /** Where agents leave from when they go offline (world px, feet). */
  door: { x: number; y: number };
}

const ROOMS: Room[] = [
  { id: "meeting", rect: { x: 1, y: 1, w: 17, h: 11 } },
  { id: "kitchen", rect: { x: 19, y: 1, w: 17, h: 11 } },
  { id: "server", rect: { x: 37, y: 1, w: 18, h: 11 } },
  { id: "office", rect: { x: 1, y: 13, w: 54, h: 12 } },
  { id: "lounge", rect: { x: 1, y: 26, w: 17, h: 7 } },
  { id: "reception", rect: { x: 19, y: 26, w: 18, h: 7 } },
  { id: "print", rect: { x: 38, y: 26, w: 17, h: 7 } },
];

/** Walls: one tile thick, with doorways left as gaps. */
const WALLS: TileRect[] = [
  // Outer shell; the bottom row keeps the entrance gap at x 27..29.
  { x: 0, y: 0, w: COLS, h: 1 },
  { x: 0, y: 0, w: 1, h: ROWS },
  { x: COLS - 1, y: 0, w: 1, h: ROWS },
  { x: 0, y: ROWS - 1, w: 27, h: 1 },
  { x: 30, y: ROWS - 1, w: COLS - 30, h: 1 },

  // Top band dividers.
  { x: 18, y: 1, w: 1, h: 12 },
  { x: 36, y: 1, w: 1, h: 12 },
  // Top band -> office, three doorways.
  { x: 1, y: 12, w: 6, h: 1 },
  { x: 10, y: 12, w: 15, h: 1 },
  { x: 28, y: 12, w: 17, h: 1 },
  { x: 48, y: 12, w: 7, h: 1 },

  // Office -> bottom band dividers.
  { x: 1, y: 25, w: 5, h: 1 },
  { x: 9, y: 25, w: 18, h: 1 },
  { x: 30, y: 25, w: 14, h: 1 },
  { x: 47, y: 25, w: 8, h: 1 },
  { x: 18, y: 26, w: 1, h: 8 },
  { x: 37, y: 26, w: 1, h: 8 },
];

/** Openings cut out of the wall runs above, in draw order. */
const DOORS: TileRect[] = [
  { x: 7, y: 12, w: 3, h: 1 },
  { x: 25, y: 12, w: 3, h: 1 },
  { x: 45, y: 12, w: 3, h: 1 },
  { x: 6, y: 25, w: 3, h: 1 },
  { x: 27, y: 25, w: 3, h: 1 },
  { x: 44, y: 25, w: 3, h: 1 },
  { x: 27, y: ROWS - 1, w: 3, h: 1 },
];

const DESK_COLUMNS = [4, 7, 22, 25, 40, 43] as const;
const DESK_ROWS = [16, 20] as const;

/** Standing room for agents beyond the desk count (tiles). */
const OVERFLOW_TILES = [
  [51, 14],
  [51, 16],
  [51, 18],
  [51, 20],
  [51, 22],
  [51, 24],
  [10, 14],
  [10, 24],
] as const;

function deskAt(x: number, y: number): AgentSlot {
  return {
    desk: { x, y, w: 2, h: 1 },
    // The chair sits north of the desk; the agent faces down at the monitor.
    x: (x + 1) * TILE,
    y: (y - 1) * TILE + 8,
    dir: "down",
  };
}

const DESKS: AgentSlot[] = DESK_ROWS.flatMap((row) =>
  DESK_COLUMNS.map((col) => deskAt(col, row)),
);

/** Home slots agents can occupy: 0-11 are desks, then the overflow spots. */
export const OFFICE_DESK_COUNT = DESKS.length;
export const OFFICE_HOME_COUNT = OFFICE_DESK_COUNT + OVERFLOW_TILES.length;

/**
 * The office floor groups its desks into three clusters (x = 4/7, 22/25,
 * 40/43). Each cluster is a zone a department can call its own; the anchor is
 * where its plaque is drawn (tiles, top-left).
 */
export interface DeskZoneSpec {
  key: string;
  /** Home slots in this cluster, in seating order. */
  homes: readonly number[];
  anchor: { tx: number; ty: number };
}

// Desk indexes are row-major (`row * DESK_COLUMNS.length + column`): the
// 16-row columns 4/7 are 0/1 and the 20-row ones 6/7, and so on. Overflow
// slots 12-17 run down the right wall, 18-19 are the two left-wall spots.
export const OFFICE_DESK_ZONES: readonly DeskZoneSpec[] = [
  { key: "west", homes: [0, 1, 6, 7, 18, 19], anchor: { tx: 5, ty: 14 } },
  { key: "center", homes: [2, 3, 8, 9], anchor: { tx: 22, ty: 14 } },
  {
    key: "east",
    homes: [4, 5, 10, 11, 12, 13, 14, 15, 16, 17],
    anchor: { tx: 40, ty: 14 },
  },
];

function seat(
  key: string,
  kind: StationKind,
  tx: number,
  ty: number,
  dir: OfficeDir,
  capacity = 1,
): Station {
  return {
    key,
    kind,
    x: tx * TILE + TILE / 2,
    y: ty * TILE + TILE - 2,
    dir,
    capacity,
    seat:
      kind === "desk" ||
      kind === "meeting" ||
      kind === "sofa" ||
      kind === "beanbag" ||
      kind === "kitchen-table",
  };
}

/** Every seat and errand spot in the floor plan. */
const STATIONS: Station[] = [
  ...DESKS.map((slot, index) =>
    seat(`desk:${index}`, "desk", Math.floor(slot.x / TILE), Math.floor(slot.y / TILE), "down"),
  ),
  // Meeting room: four seats along each long side of the table.
  ...[6, 8, 10, 12].map((tx) => seat(`meeting:n:${tx}`, "meeting", tx, 4, "down")),
  ...[6, 8, 10, 12].map((tx) => seat(`meeting:s:${tx}`, "meeting", tx, 7, "up")),
  // Pantry: machines along the counter, stools around the high table.
  seat("coffee", "coffee", 23, 3, "up"),
  seat("water", "water", 29, 3, "up"),
  seat("snack", "snack", 32, 3, "up"),
  ...[25, 27].map((tx) => seat(`pantry:n:${tx}`, "kitchen-table", tx, 6, "down")),
  ...[25, 27].map((tx) => seat(`pantry:s:${tx}`, "kitchen-table", tx, 9, "up")),
  // Server room: operator spots in front of both rack rows.
  seat("server:0", "server", 42, 3, "up"),
  seat("server:1", "server", 49, 3, "up"),
  seat("server:2", "server", 42, 8, "down"),
  seat("server:3", "server", 49, 8, "down"),
  // Lounge: the north sofa takes sitters; the south one is the backdrop to
  // the low table, so it stays decorative and keeps the room readable.
  ...[3, 4, 5, 6].map((tx) => seat(`sofa:a:${tx}`, "sofa", tx, 28, "up")),
  seat("beanbag", "beanbag", 8, 31, "up"),
  // Front desk and print corner.
  seat("reception", "reception", 26, 26, "down"),
  seat("printer:0", "printer", 42, 27, "up"),
  seat("printer:1", "printer", 46, 27, "up"),
  // Watering spots beside the office plants.
  seat("plant:0", "plant", 2, 14, "up"),
  seat("plant:1", "plant", 20, 14, "up"),
  seat("plant:2", "plant", 38, 14, "up"),
  seat("plant:3", "plant", 53, 14, "up"),
  seat("door", "door", 28, ROWS - 2, "down"),
];

const STOOLS: Furniture[] = [
  [25, 6],
  [27, 6],
  [25, 9],
  [27, 9],
].map(([x, y], index) => ({
  id: `pantry-stool:${index}`,
  kind: "stool" as const,
  rect: { x: x!, y: y!, w: 1, h: 1 },
  blocks: false,
}));

const FURNITURE: Furniture[] = [
  ...DESKS.map((slot, index) => ({
    id: `desk:${index}`,
    kind: "desk" as const,
    rect: slot.desk,
    blocks: true,
  })),

  // Meeting room: board, presentation screen, wall clock, sideboard.
  { id: "meeting-table", kind: "meeting-table", rect: { x: 5, y: 5, w: 8, h: 2 }, blocks: true },
  { id: "meeting-board", kind: "whiteboard", rect: { x: 5, y: 1, w: 6, h: 1 }, blocks: true },
  { id: "meeting-screen", kind: "screen", rect: { x: 14, y: 1, w: 3, h: 1 }, blocks: true },
  { id: "meeting-clock", kind: "clock", rect: { x: 3, y: 1, w: 1, h: 1 }, blocks: false },
  { id: "meeting-sideboard", kind: "bookshelf", rect: { x: 15, y: 3, w: 1, h: 1 }, blocks: true },
  { id: "meeting-plant-a", kind: "plant", rect: { x: 2, y: 2, w: 1, h: 1 }, blocks: true },
  { id: "meeting-plant-b", kind: "plant", rect: { x: 16, y: 10, w: 1, h: 1 }, blocks: true },

  // Pantry: coffee machine, kettle corner, water, fridge, vending, high table.
  { id: "pantry-counter", kind: "counter", rect: { x: 21, y: 2, w: 13, h: 1 }, blocks: true },
  {
    id: "pantry-coffee",
    kind: "coffee-machine",
    rect: { x: 23, y: 2, w: 2, h: 1 },
    blocks: false,
  },
  { id: "pantry-water", kind: "water-cooler", rect: { x: 29, y: 2, w: 1, h: 1 }, blocks: false },
  { id: "pantry-fridge", kind: "fridge", rect: { x: 32, y: 2, w: 2, h: 1 }, blocks: false },
  { id: "pantry-shelf", kind: "bookshelf", rect: { x: 34, y: 2, w: 1, h: 1 }, blocks: true },
  { id: "pantry-vending", kind: "vending", rect: { x: 21, y: 5, w: 2, h: 1 }, blocks: true },
  { id: "pantry-table", kind: "kitchen-table", rect: { x: 24, y: 7, w: 4, h: 2 }, blocks: true },
  ...STOOLS,
  { id: "pantry-plant", kind: "plant", rect: { x: 19, y: 10, w: 1, h: 1 }, blocks: true },

  // Server room: two rows of racks flanking the aisle.
  { id: "server-rack-a", kind: "rack", rect: { x: 39, y: 2, w: 6, h: 1 }, blocks: true },
  { id: "server-rack-b", kind: "rack", rect: { x: 46, y: 2, w: 6, h: 1 }, blocks: true },
  { id: "server-rack-c", kind: "rack", rect: { x: 39, y: 9, w: 6, h: 1 }, blocks: true },
  { id: "server-rack-d", kind: "rack", rect: { x: 46, y: 9, w: 6, h: 1 }, blocks: true },
  { id: "server-console", kind: "counter", rect: { x: 52, y: 5, w: 2, h: 1 }, blocks: true },
  { id: "server-plant", kind: "plant", rect: { x: 38, y: 10, w: 1, h: 1 }, blocks: true },

  // Open office: credenzas and plants between the desk clusters.
  { id: "office-credenza-a", kind: "bookshelf", rect: { x: 11, y: 13, w: 3, h: 1 }, blocks: true },
  { id: "office-credenza-b", kind: "bookshelf", rect: { x: 32, y: 13, w: 3, h: 1 }, blocks: true },
  { id: "office-water", kind: "water-cooler", rect: { x: 12, y: 24, w: 1, h: 1 }, blocks: false },
  { id: "office-plant-a", kind: "plant", rect: { x: 2, y: 13, w: 1, h: 1 }, blocks: true },
  { id: "office-plant-b", kind: "plant", rect: { x: 19, y: 13, w: 1, h: 1 }, blocks: true },
  { id: "office-plant-c", kind: "plant", rect: { x: 37, y: 13, w: 1, h: 1 }, blocks: true },
  { id: "office-plant-d", kind: "plant", rect: { x: 53, y: 13, w: 1, h: 1 }, blocks: true },
  { id: "office-plant-e", kind: "plant", rect: { x: 2, y: 24, w: 1, h: 1 }, blocks: true },
  { id: "office-plant-f", kind: "plant", rect: { x: 53, y: 24, w: 1, h: 1 }, blocks: true },

  // Lounge: two facing sofas, low table, lamp, shelf, aquarium, beanbag.
  { id: "lounge-sofa-a", kind: "sofa", rect: { x: 3, y: 27, w: 4, h: 1 }, blocks: true },
  { id: "lounge-table", kind: "coffee-table", rect: { x: 3, y: 29, w: 4, h: 1 }, blocks: true },
  { id: "lounge-sofa-b", kind: "sofa", rect: { x: 3, y: 31, w: 4, h: 1 }, blocks: true },
  { id: "lounge-lamp", kind: "lamp", rect: { x: 8, y: 27, w: 1, h: 1 }, blocks: false },
  { id: "lounge-shelf", kind: "bookshelf", rect: { x: 10, y: 27, w: 2, h: 1 }, blocks: true },
  { id: "lounge-aquarium", kind: "aquarium", rect: { x: 13, y: 27, w: 2, h: 1 }, blocks: true },
  { id: "lounge-beanbag", kind: "beanbag", rect: { x: 8, y: 31, w: 1, h: 1 }, blocks: false },
  { id: "lounge-plant-a", kind: "plant", rect: { x: 2, y: 32, w: 1, h: 1 }, blocks: true },
  { id: "lounge-plant-b", kind: "plant", rect: { x: 15, y: 32, w: 1, h: 1 }, blocks: true },

  // Reception: desk, waiting sofa, low table, poster.
  { id: "reception-desk", kind: "reception-desk", rect: { x: 22, y: 27, w: 6, h: 1 }, blocks: true },
  { id: "reception-sofa", kind: "sofa", rect: { x: 31, y: 28, w: 3, h: 1 }, blocks: true },
  { id: "reception-table", kind: "coffee-table", rect: { x: 30, y: 31, w: 4, h: 1 }, blocks: true },
  { id: "reception-poster", kind: "poster", rect: { x: 33, y: 26, w: 2, h: 1 }, blocks: false },
  { id: "reception-plant", kind: "plant", rect: { x: 21, y: 31, w: 1, h: 1 }, blocks: true },

  // Print corner: two printers, a trimmer counter, shelves.
  { id: "print-printer-a", kind: "printer", rect: { x: 41, y: 26, w: 2, h: 1 }, blocks: true },
  { id: "print-printer-b", kind: "printer", rect: { x: 45, y: 26, w: 2, h: 1 }, blocks: true },
  { id: "print-rack", kind: "rack", rect: { x: 50, y: 26, w: 4, h: 1 }, blocks: true },
  { id: "print-counter", kind: "counter", rect: { x: 40, y: 29, w: 3, h: 1 }, blocks: true },
  { id: "print-shelf", kind: "bookshelf", rect: { x: 47, y: 29, w: 3, h: 1 }, blocks: true },
  { id: "print-plant", kind: "plant", rect: { x: 52, y: 31, w: 1, h: 1 }, blocks: true },
];

/**
 * Office upgrades the company earns as it levels up. The scene rebuilds its
 * furniture when the company level changes, so these also join the obstacle
 * set and agents path around them like any other piece of the office.
 */
const DECORATIONS: { level: number; item: Furniture }[] = [
  {
    level: 2,
    item: {
      id: "unlock-plant",
      kind: "plant",
      rect: { x: 35, y: 31, w: 1, h: 1 },
      blocks: true,
    },
  },
  {
    level: 3,
    item: {
      id: "unlock-poster",
      kind: "poster",
      rect: { x: 48, y: 13, w: 2, h: 1 },
      blocks: false,
    },
  },
  {
    level: 4,
    item: {
      id: "unlock-shelf",
      kind: "bookshelf",
      rect: { x: 12, y: 31, w: 2, h: 1 },
      blocks: true,
    },
  },
];

function isBlockedTile(layout: OfficeLayout, tx: number, ty: number): boolean {
  return layout.obstacles.some(
    (rect) => tx >= rect.x && tx < rect.x + rect.w && ty >= rect.y && ty < rect.y + rect.h,
  );
}

/** True when the tile exists and has no furniture or wall on it. */
export function isWalkableTile(layout: OfficeLayout, tx: number, ty: number): boolean {
  if (tx < 0 || ty < 0 || tx >= COLS || ty >= ROWS) return false;
  return !isBlockedTile(layout, tx, ty);
}

/** Level-gated decorations the company has unlocked, lowest level first. */
export function unlockedDecorations(companyLevel: number): Furniture[] {
  return DECORATIONS.filter((entry) => companyLevel >= entry.level).map(
    (entry) => entry.item,
  );
}

export function buildOfficeLayout(companyLevel = 1): OfficeLayout {
  const furniture = [...FURNITURE, ...unlockedDecorations(companyLevel)];
  const obstacles = [
    ...WALLS,
    ...furniture.filter((item) => item.blocks).map((item) => item.rect),
  ];

  return {
    agents: DESKS,
    overflowSpots: OVERFLOW_TILES.map(([tx, ty]) => ({
      x: tx * TILE + TILE / 2,
      y: ty * TILE + TILE - 2,
      dir: "left" as OfficeDir,
    })),
    rooms: ROOMS,
    furniture,
    walls: WALLS,
    doors: DOORS,
    stations: STATIONS,
    obstacles,
    spawns: [27, 28, 29].map((tx) => ({
      x: tx * TILE + TILE / 2,
      y: (ROWS - 2) * TILE + TILE - 2,
    })),
    door: { x: 28 * TILE + TILE / 2, y: (ROWS - 2) * TILE + TILE - 2 },
  };
}

/** The station with a given key, or null when the layout has no such spot. */
export function stationByKey(layout: OfficeLayout, key: string): Station | null {
  return layout.stations.find((station) => station.key === key) ?? null;
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
 * empty array when the target is blocked or unreachable. The floor plan is
 * 56 x 34 tiles, so an exhaustive BFS is cheaper than any smarter structure
 * and never needs re-planning mid-walk.
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
  return { x: spot?.x ?? 0, y: spot?.y ?? 0, dir: "up" };
}
