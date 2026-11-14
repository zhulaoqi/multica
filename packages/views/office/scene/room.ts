import type { Container, Graphics } from "pixi.js";
import { COLS, ROWS, TILE, type OfficeLayout } from "./layout";

/**
 * The room is drawn from code — no sprite sheets — so it ships with the
 * bundle as a few hundred rectangles and keeps working offline.
 */
export const PALETTE = {
  floorA: 0xd9c6a7,
  floorB: 0xd2bd9c,
  floorLine: 0xc4ad8a,
  wall: 0x6f7f8a,
  wallDark: 0x5a6972,
  window: 0xbfe3ef,
  windowFrame: 0x55666f,
  whiteboard: 0xf4f6f8,
  whiteboardInk: [0x4f9bd9, 0xd97a7a, 0x63b37a],
  rug: 0x9fcec1,
  rugBorder: 0x86b8aa,
  desk: 0xba854f,
  deskTop: 0xc99a63,
  deskLeg: 0x8d6238,
  monitor: 0x33414d,
  screen: 0x7cc0d8,
  chair: 0x4e6675,
  plantPot: 0xb06e4f,
  leafLight: 0x6aa877,
  leafDark: 0x4f8a5f,
  sofa: 0xd08a7c,
  sofaDark: 0xb57367,
  table: 0xcbb899,
  tableEdge: 0xb5a183,
  counter: 0xa9b3bb,
  counterDark: 0x8a949c,
  coffee: 0x3f4a54,
} as const;

const SHIRT_COLORS = [0x4f9bd9, 0x63b37a, 0xd97a7a, 0xb07ad9, 0xd9a34f, 0x5aa8a2] as const;
const SKIN_COLORS = [0xf1c9a5, 0xe0b089, 0xc68f63, 0x9c6b45] as const;
const HAIR_COLORS = [0x3a2a22, 0x5a3d2b, 0x8a6a45, 0x2d2d33] as const;

export function hashSeed(value: string): number {
  let hash = 0;
  for (let index = 0; index < value.length; index++) {
    hash = (hash * 31 + value.charCodeAt(index)) % 100_003;
  }
  return hash;
}

export function pickColor(palette: readonly number[], seed: number): number {
  return palette[Math.abs(seed) % palette.length] ?? 0x000000;
}

export const characterColors = {
  shirt: (seed: number) => pickColor(SHIRT_COLORS, seed),
  skin: (seed: number) => pickColor(SKIN_COLORS, seed >> 3),
  hair: (seed: number) => pickColor(HAIR_COLORS, seed >> 6),
};

function rect(
  graphics: Graphics,
  x: number,
  y: number,
  width: number,
  height: number,
  color: number,
  alpha = 1,
) {
  graphics.rect(x, y, width, height).fill({ color, alpha });
}

function drawFloor(graphics: Graphics) {
  for (let ty = 0; ty < ROWS; ty++) {
    for (let tx = 0; tx < COLS; tx++) {
      rect(
        graphics,
        tx * TILE,
        ty * TILE,
        TILE,
        TILE,
        (tx + ty) % 2 === 0 ? PALETTE.floorA : PALETTE.floorB,
      );
    }
  }
  for (let ty = 0; ty < ROWS; ty++) {
    rect(graphics, 0, ty * TILE + TILE - 1, COLS * TILE, 1, PALETTE.floorLine, 0.35);
  }
}

function drawWalls(graphics: Graphics) {
  // Top wall with a strip of windows, left and right walls, and a bottom wall
  // with the entrance gap the spawn points stand in.
  rect(graphics, 0, 0, COLS * TILE, TILE, PALETTE.wall);
  rect(graphics, 0, 0, TILE, ROWS * TILE, PALETTE.wallDark);
  rect(graphics, (COLS - 1) * TILE, 0, TILE, ROWS * TILE, PALETTE.wallDark);
  rect(graphics, 0, (ROWS - 1) * TILE, 13 * TILE, TILE, PALETTE.wallDark);
  rect(graphics, 17 * TILE, (ROWS - 1) * TILE, (COLS - 17) * TILE, TILE, PALETTE.wallDark);

  for (const windowX of [6, 11, 16]) {
    rect(graphics, windowX * TILE, 3, 4 * TILE, 10, PALETTE.windowFrame);
    rect(graphics, windowX * TILE + 1, 4, 4 * TILE - 2, 8, PALETTE.window);
  }
}

function drawWhiteboard(graphics: Graphics, layout: OfficeLayout) {
  const board = layout.whiteboard;
  rect(graphics, board.x * TILE, TILE + 2, board.w * TILE, 12, PALETTE.whiteboard);
  PALETTE.whiteboardInk.forEach((color, index) => {
    rect(graphics, board.x * TILE + 6 + index * 14, TILE + 5, 10, 2, color);
    rect(graphics, board.x * TILE + 6 + index * 14, TILE + 9, 7, 2, color, 0.6);
  });
}

function drawDesk(graphics: Graphics, deskX: number, deskY: number) {
  const x = deskX * TILE;
  const y = deskY * TILE;
  rect(graphics, x, y, 2 * TILE, TILE, PALETTE.desk);
  rect(graphics, x, y, 2 * TILE, 4, PALETTE.deskTop);
  rect(graphics, x + 1, y + TILE - 3, 3, 3, PALETTE.deskLeg);
  rect(graphics, x + 2 * TILE - 4, y + TILE - 3, 3, 3, PALETTE.deskLeg);
  // Monitor on the far side of the desk, facing the agent.
  rect(graphics, x + 8, y - 12, 16, 11, PALETTE.monitor);
  rect(graphics, x + 10, y - 10, 12, 7, PALETTE.screen);
  rect(graphics, x + 14, y - 1, 4, 2, PALETTE.monitor);
}

function drawChair(graphics: Graphics, slotX: number, slotY: number) {
  rect(graphics, slotX - 7, slotY - 8, 14, 5, PALETTE.chair);
  rect(graphics, slotX - 5, slotY - 3, 10, 3, PALETTE.chair);
}

function drawPlant(graphics: Graphics, x: number, y: number) {
  const px = x * TILE;
  const py = y * TILE;
  rect(graphics, px + 4, py + 9, 8, 6, PALETTE.plantPot);
  graphics.circle(px + 5, py + 7, 4).fill(PALETTE.leafDark);
  graphics.circle(px + 11, py + 6, 4).fill(PALETTE.leafLight);
  graphics.circle(px + 8, py + 3, 4).fill(PALETTE.leafLight);
}

function drawFurniture(graphics: Graphics, layout: OfficeLayout) {
  for (const slot of layout.agents) {
    drawDesk(graphics, slot.desk.x, slot.desk.y);
    drawChair(graphics, slot.x, slot.y);
  }
  for (const plant of layout.plants) drawPlant(graphics, plant.x, plant.y);

  const table = layout.meetingTable;
  rect(graphics, table.x * TILE, table.y * TILE, table.w * TILE, table.h * TILE, PALETTE.tableEdge);
  rect(
    graphics,
    table.x * TILE + 2,
    table.y * TILE + 2,
    table.w * TILE - 4,
    table.h * TILE - 4,
    PALETTE.table,
  );
  for (const offset of [0.5, 2.5]) {
    drawChair(graphics, table.x * TILE + offset * TILE, table.y * TILE - 4);
  }

  const sofa = layout.sofa;
  rect(graphics, sofa.x * TILE, sofa.y * TILE, sofa.w * TILE, sofa.h * TILE, PALETTE.sofaDark);
  rect(graphics, sofa.x * TILE, sofa.y * TILE + 3, sofa.w * TILE, sofa.h * TILE - 4, PALETTE.sofa);

  const counter = layout.counter;
  rect(graphics, counter.x * TILE, counter.y * TILE, counter.w * TILE, counter.h * TILE, PALETTE.counterDark);
  rect(graphics, counter.x * TILE, counter.y * TILE, counter.w * TILE, 4, PALETTE.counter);
  rect(graphics, counter.x * TILE + 4, counter.y * TILE - 6, 8, 7, PALETTE.coffee);
}

/** Builds the static room: one container with a floor layer and a furniture layer. */
export function drawRoom(
  pixi: typeof import("pixi.js"),
  layout: OfficeLayout,
): Container {
  const room = new pixi.Container();

  const rug = new pixi.Graphics();
  rect(
    rug,
    layout.rug.x * TILE,
    layout.rug.y * TILE,
    layout.rug.w * TILE,
    layout.rug.h * TILE,
    PALETTE.rugBorder,
  );
  rect(
    rug,
    layout.rug.x * TILE + 3,
    layout.rug.y * TILE + 3,
    layout.rug.w * TILE - 6,
    layout.rug.h * TILE - 6,
    PALETTE.rug,
    0.85,
  );
  room.addChild(rug);

  const floor = new pixi.Graphics();
  drawFloor(floor);
  room.addChild(floor);

  const walls = new pixi.Graphics();
  drawWalls(walls);
  drawWhiteboard(walls, layout);
  room.addChild(walls);

  const furniture = new pixi.Graphics();
  drawFurniture(furniture, layout);
  room.addChild(furniture);

  return room;
}
