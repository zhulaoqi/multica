import type { Container, Graphics, NineSliceSprite, Sprite } from "pixi.js";
import { COLS, ROWS, TILE, type Furniture, type OfficeLayout, type RoomId } from "./layout";
import { officeTextures, type FurnitureSprite } from "./textures";
import { WALL_FILL } from "./assets/frames";

/**
 * The office is drawn from the pixel-agents tile set: sprite floors, walls
 * autotiled from their neighbours, and sprite furniture. Machines the pack
 * has no art for are drawn as outlined pixel rectangles in the same style.
 */

export function hashSeed(value: string): number {
  let hash = 0;
  for (let index = 0; index < value.length; index++) {
    hash = (hash * 31 + value.charCodeAt(index)) % 100_003;
  }
  return hash;
}

const OUTLINE = 0x1f2430;

const WALL_N = 1;
const WALL_E = 2;
const WALL_S = 4;
const WALL_W = 8;

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

/** A filled block with the 1 px dark outline the sprite art uses. */
function box(graphics: Graphics, x: number, y: number, width: number, height: number, fill: number) {
  rect(graphics, x, y, width, height, OUTLINE);
  rect(graphics, x + 1, y + 1, width - 2, height - 2, fill);
}

/** Wall plaques beside each doorway, so every room reads at a glance. */
const DOOR_SIGNS: { room: RoomId; tx: number; ty: number }[] = [
  { room: "meeting", tx: 10, ty: 12 },
  { room: "kitchen", tx: 28, ty: 12 },
  { room: "server", tx: 48, ty: 12 },
  { room: "lounge", tx: 9, ty: 25 },
  { room: "reception", tx: 30, ty: 25 },
  { room: "print", tx: 47, ty: 25 },
];

const SIGN_TINTS: Record<RoomId, number> = {
  meeting: 0x4f9bd9,
  kitchen: 0xd9a34f,
  server: 0x63b37a,
  office: 0x5aa8a2,
  lounge: 0xd97a7a,
  reception: 0xb07ad9,
  print: 0x9aa6b0,
};

function drawSignIcon(graphics: Graphics, room: RoomId, x: number, y: number) {
  switch (room) {
    case "meeting":
      rect(graphics, x, y + 4, 7, 2, 0xe8f6fb);
      rect(graphics, x, y, 2, 2, 0xbfe3ef);
      rect(graphics, x + 5, y, 2, 2, 0xbfe3ef);
      break;
    case "kitchen":
      rect(graphics, x + 1, y + 1, 4, 5, 0xf8fafb);
      rect(graphics, x + 5, y + 2, 2, 1, 0xf8fafb);
      rect(graphics, x + 2, y, 2, 1, 0xffffff, 0.7);
      break;
    case "server":
      for (let row = 0; row < 3; row++) {
        rect(graphics, x, y + row * 2, 6, 1, 0xcbd5e1);
        rect(graphics, x + 5, y + row * 2, 1, 1, 0x8ef0b0);
      }
      break;
    case "lounge":
      rect(graphics, x, y + 1, 7, 2, 0xf4d7d0);
      rect(graphics, x, y + 3, 7, 3, 0xe9b5ab);
      break;
    case "reception":
      rect(graphics, x, y + 5, 7, 2, 0xe9d3b4);
      rect(graphics, x + 2, y + 1, 3, 3, 0xf1d7b0);
      break;
    case "print":
      rect(graphics, x, y + 2, 6, 3, 0xd7dde2);
      rect(graphics, x + 1, y + 5, 4, 2, 0xf8fafb);
      break;
    default:
      break;
  }
}

function drawDoorSign(graphics: Graphics, room: RoomId, tx: number, ty: number) {
  const x = tx * TILE + 1;
  const y = ty * TILE - 4;
  box(graphics, x, y, 14, 11, 0x24323e);
  rect(graphics, x + 1, y + 1, 2, 9, SIGN_TINTS[room]);
  drawSignIcon(graphics, room, x + 5, y + 2);
}

function drawWindow(graphics: Graphics, x: number, y: number, width: number) {
  box(graphics, x, y, width, 11, 0x9fd3e6);
  rect(graphics, x + 1, y + 1, width - 2, 3, 0xd8f1fa);
  for (let pane = x + 16; pane < x + width - 4; pane += 16) {
    rect(graphics, pane, y + 1, 1, 9, OUTLINE);
  }
  rect(graphics, x - 1, y + 11, width + 2, 2, 0x8a949c);
}

function drawCoffeeMachine(graphics: Graphics, x: number, y: number) {
  const px = x * TILE;
  const py = y * TILE;
  box(graphics, px + 1, py - 10, 22, 16, 0x3f4a54);
  rect(graphics, px + 3, py - 8, 18, 5, 0x55636f);
  rect(graphics, px + 5, py - 7, 6, 3, 0x7cc0d8);
  rect(graphics, px + 14, py - 7, 3, 2, 0xd9a34f);
  rect(graphics, px + 8, py - 2, 8, 6, 0x2a323d);
  box(graphics, px + 9, py + 1, 6, 4, 0xf8fafb);
}

function drawWaterCooler(graphics: Graphics, x: number, y: number) {
  const px = x * TILE + 2;
  const py = y * TILE;
  box(graphics, px, py - 13, 12, 11, 0xa8d8ef);
  rect(graphics, px + 2, py - 11, 3, 6, 0xd8f1fa);
  box(graphics, px + 1, py - 3, 10, 13, 0xc4ccd3);
  rect(graphics, px + 3, py + 1, 6, 3, 0x4f9bd9);
}

function drawFridge(graphics: Graphics, x: number, y: number) {
  const px = x * TILE;
  const py = y * TILE;
  box(graphics, px, py - 18, 26, 26, 0xd7dde2);
  rect(graphics, px + 1, py - 17, 24, 2, 0xeef2f5);
  rect(graphics, px + 1, py - 7, 24, 1, 0x8a949c);
  rect(graphics, px + 21, py - 14, 2, 5, 0x6b7884);
  rect(graphics, px + 21, py - 4, 2, 5, 0x6b7884);
}

function drawPrinter(graphics: Graphics, x: number, y: number) {
  const px = x * TILE;
  const py = y * TILE;
  box(graphics, px + 1, py - 12, 28, 18, 0xb9c3cb);
  rect(graphics, px + 2, py - 11, 26, 5, 0xdde4e9);
  rect(graphics, px + 5, py - 8, 12, 2, 0x6b7884);
  box(graphics, px + 7, py + 1, 16, 5, 0xf8fafb);
  rect(graphics, px + 9, py + 3, 12, 1, 0x9aa6b0);
  rect(graphics, px + 24, py - 9, 2, 2, 0x2f5f3f);
}

function drawRack(graphics: Graphics, x: number, y: number, w: number) {
  const px = x * TILE;
  const py = y * TILE;
  box(graphics, px, py - 22, w * TILE, 30, 0x3b4552);
  for (let row = 0; row < 4; row++) {
    rect(graphics, px + 3, py - 18 + row * 6, w * TILE - 6, 4, 0x232a33);
    for (let dot = 0; dot < Math.min(6, w); dot++) {
      rect(graphics, px + 5 + dot * 8, py - 17 + row * 6, 2, 2, 0x2f4f3f);
    }
  }
}

/** A drinks machine: tall body, lit window, and a dispenser slot. */
function drawVending(graphics: Graphics, x: number, y: number) {
  const px = x * TILE;
  const py = y * TILE;
  box(graphics, px + 1, py - 26, 30, 30, 0xb54a4a);
  box(graphics, px + 3, py - 25, 26, 15, 0x1f2a33);
  const cans = [0xd97a7a, 0x4f9bd9, 0x63b37a, 0xd9a34f, 0xb07ad9, 0x5aa8a2];
  for (let row = 0; row < 2; row++) {
    for (let col = 0; col < 4; col++) {
      rect(graphics, px + 5 + col * 6, py - 23 + row * 6, 4, 4, cans[(row * 4 + col) % cans.length]!);
    }
  }
  box(graphics, px + 4, py - 8, 12, 8, 0x2b3642);
  box(graphics, px + 19, py - 8, 8, 5, 0xe8e2d4);
}

function drawLamp(graphics: Graphics, x: number, y: number) {
  const cx = x * TILE + TILE / 2;
  const cy = y * TILE + TILE - 3;
  box(graphics, cx - 4, cy - 1, 8, 3, 0x6b7884);
  box(graphics, cx - 1, cy - 21, 3, 21, 0x6b7884);
  box(graphics, cx - 6, cy - 27, 13, 7, 0xf2d3a0);
  rect(graphics, cx - 5, cy - 26, 11, 2, 0xffe8bb);
}

function drawAquarium(graphics: Graphics, x: number, y: number, w: number) {
  const px = x * TILE;
  const py = y * TILE;
  const pw = w * TILE;
  box(graphics, px, py - 7, pw, 21, 0x8fd3e8);
  rect(graphics, px + 1, py - 6, pw - 2, 3, 0xbfe3ef);
  rect(graphics, px + 1, py + 9, pw - 2, 4, 0xcbb899);
  rect(graphics, px + 4, py + 2, 1, 7, 0x4f8a5f);
  rect(graphics, px + 6, py + 1, 1, 8, 0x6aa877);
  rect(graphics, px + pw - 6, py + 3, 1, 6, 0x4f8a5f);
  rect(graphics, px + pw - 8, py + 2, 1, 7, 0x6aa877);
  box(graphics, px - 1, py + 13, pw + 2, 4, 0x53606b);
}

function drawBeanbag(graphics: Graphics, x: number, y: number) {
  const cx = x * TILE + TILE / 2;
  const cy = y * TILE + TILE / 2 + 1;
  graphics.ellipse(cx, cy, 8, 6.5).fill(OUTLINE);
  graphics.ellipse(cx, cy, 7, 5.5).fill(0x8a6f9c);
  graphics.ellipse(cx - 1, cy - 1.5, 5, 3.5).fill(0xb08fc4);
}

/** The presentation panel in the meeting room: frame, slide, chart bars. */
function drawPresentationScreen(graphics: Graphics, x: number, y: number, w: number) {
  const px = x * TILE;
  const py = y * TILE;
  const pw = w * TILE;
  box(graphics, px, py + 1, pw, 14, 0x1f2a33);
  rect(graphics, px + 2, py + 3, pw - 4, 10, 0x2f6f8a);
  rect(graphics, px + 4, py + 4, 14, 2, 0xe8f6fb);
  for (let index = 0; index < 3; index++) {
    rect(graphics, px + 4, py + 8 + index * 2, 18 - index * 4, 1, 0xbfe3ef);
  }
  rect(graphics, px + pw - 10, py + 5, 6, 6, 0xd9a34f);
}

/** Lights the edges of a monitor seen from behind while its agent works. */
export interface DeskScreen {
  setOn(on: boolean, nowMs: number): void;
}

export interface RoomView {
  /** Floor tiles, drawn at the bottom of the world layer. */
  floor: Container;
  /** Sortable walls and props, each already positioned with a `zIndex`. */
  props: Container[];
  /** One monitor per desk, in `layout.agents` order. */
  screens: DeskScreen[];
  /** Advances the looping ambience: steam, LEDs, clock hands, fish, glow. */
  update(nowMs: number): void;
}

/**
 * Builds the static office: a floor tile pass, autotiled wall pieces, and one
 * sortable node per furniture sprite so characters can walk in front of and
 * behind them. `loadOfficeTextures` must have resolved first.
 */
export function buildRoom(pixi: typeof import("pixi.js"), layout: OfficeLayout): RoomView {
  const textures = officeTextures();
  const props: Container[] = [];
  const screens: DeskScreen[] = [];

  const place = (name: FurnitureSprite, x: number, y: number, zIndex: number): Sprite => {
    const sprite = new pixi.Sprite(textures.furniture[name]);
    sprite.position.set(x, y);
    sprite.zIndex = zIndex;
    props.push(sprite);
    return sprite;
  };

  /** A 9-slice cut from part of a furniture frame, stretched to size. */
  const stretch = (
    name: FurnitureSprite,
    crop: readonly [number, number, number, number],
    slices: readonly [number, number, number, number],
    x: number,
    y: number,
    width: number,
    height: number,
    zIndex: number,
  ): NineSliceSprite => {
    const base = textures.furniture[name];
    const texture = new pixi.Texture({
      source: base.source,
      frame: new pixi.Rectangle(base.frame.x + crop[0], base.frame.y + crop[1], crop[2], crop[3]),
    });
    const [leftWidth, topHeight, rightWidth, bottomHeight] = slices;
    const sprite = new pixi.NineSliceSprite({
      texture,
      leftWidth,
      topHeight,
      rightWidth,
      bottomHeight,
      width,
      height,
    });
    sprite.position.set(x, y);
    sprite.zIndex = zIndex;
    props.push(sprite);
    return sprite;
  };

  const drawn = (zIndex: number, draw: (graphics: Graphics) => void) => {
    const graphics = new pixi.Graphics();
    draw(graphics);
    graphics.zIndex = zIndex;
    props.push(graphics);
    return graphics;
  };

  /** A wooden worktop: the desk sprite stretched along a tile run. */
  const worktop = (x: number, y: number, w: number, zIndex: number) =>
    stretch("deskFront", [2, 11, 44, 21], [4, 12, 4, 8], x * TILE, y * TILE - 5, w * TILE, 21, zIndex);

  // -- floor and walls ----------------------------------------------------

  const wallTiles = new Set<string>();
  for (const wall of layout.walls) {
    for (let ty = wall.y; ty < wall.y + wall.h; ty++) {
      for (let tx = wall.x; tx < wall.x + wall.w; tx++) wallTiles.add(`${tx},${ty}`);
    }
  }
  for (const door of layout.doors) {
    for (let ty = door.y; ty < door.y + door.h; ty++) {
      for (let tx = door.x; tx < door.x + door.w; tx++) wallTiles.delete(`${tx},${ty}`);
    }
  }
  const isWall = (tx: number, ty: number) => wallTiles.has(`${tx},${ty}`);
  const roomAt = (tx: number, ty: number) =>
    layout.rooms.find(
      (room) =>
        tx >= room.rect.x &&
        tx < room.rect.x + room.rect.w &&
        ty >= room.rect.y &&
        ty < room.rect.y + room.rect.h,
    ) ?? null;

  const floor = new pixi.Container();
  const base = new pixi.Graphics();
  rect(base, 0, 0, COLS * TILE, ROWS * TILE, WALL_FILL);
  floor.addChild(base);
  for (let ty = 0; ty < ROWS; ty++) {
    for (let tx = 0; tx < COLS; tx++) {
      if (isWall(tx, ty)) continue;
      // Doorways take the floor of the room they open into.
      const room = roomAt(tx, ty) ?? roomAt(tx, ty + 1) ?? roomAt(tx, ty - 1);
      if (!room) continue;
      const tile = new pixi.Sprite(textures.floors[room.id]);
      tile.position.set(tx * TILE, ty * TILE);
      floor.addChild(tile);
    }
  }

  for (const key of wallTiles) {
    const [tx, ty] = key.split(",").map(Number) as [number, number];
    const mask =
      (isWall(tx, ty - 1) ? WALL_N : 0) |
      (isWall(tx + 1, ty) ? WALL_E : 0) |
      (isWall(tx, ty + 1) ? WALL_S : 0) |
      (isWall(tx - 1, ty) ? WALL_W : 0);
    const piece = new pixi.Sprite(textures.walls[mask]);
    piece.position.set(tx * TILE, ty * TILE - TILE);
    piece.zIndex = (ty + 1) * TILE;
    props.push(piece);
  }

  // Windows on the front face of the outer top wall.
  drawn(TILE + 1, (graphics) => {
    for (const tx of [3, 10, 21, 39, 46]) drawWindow(graphics, tx * TILE, 2, 5 * TILE);
  });
  for (const sign of DOOR_SIGNS) {
    drawn((sign.ty + 1) * TILE + 1, (graphics) => drawDoorSign(graphics, sign.room, sign.tx, sign.ty));
  }

  // -- ambience -----------------------------------------------------------

  const effects: { graphics: Graphics; draw: (seconds: number) => void }[] = [];
  // Each effect owns a thin overlay sorted with the props, so a character
  // walking past still covers the machine it is standing at.
  const addEffect = (zIndex: number, draw: (graphics: Graphics, seconds: number) => void) => {
    const graphics = new pixi.Graphics();
    graphics.zIndex = zIndex;
    effects.push({
      graphics,
      draw: (seconds) => {
        graphics.clear();
        draw(graphics, seconds);
      },
    });
    props.push(graphics);
  };

  const coffeeX = 23 * TILE + 10;
  const coffeeY = 2 * TILE;
  addEffect(coffeeY + TILE + 1, (graphics, seconds) => {
    for (let index = 0; index < 3; index++) {
      const phase = (seconds * 0.45 + index / 3) % 1;
      const x = Math.round(coffeeX + index * 4 + Math.sin(seconds * 1.6 + index) * 1.6);
      const y = Math.round(coffeeY - 12 - phase * 8);
      graphics.rect(x, y, 1 + Math.round(phase), 1 + Math.round(phase)).fill({
        color: 0xf1f5f9,
        alpha: 0.6 * (1 - phase),
      });
    }
  });

  const drawBubbles = (graphics: Graphics, seconds: number, x: number, y: number) => {
    for (let index = 0; index < 3; index++) {
      const phase = (seconds * 0.55 + index / 3) % 1;
      graphics
        .rect(x + (index % 2) * 3, Math.round(y - phase * 8), 1, 1)
        .fill({ color: 0xffffff, alpha: 0.8 * (1 - phase) });
    }
  };
  addEffect(2 * TILE + TILE + 1, (graphics, seconds) =>
    drawBubbles(graphics, seconds, 29 * TILE + 7, 2 * TILE - 4),
  );
  addEffect(24 * TILE + TILE + 1, (graphics, seconds) =>
    drawBubbles(graphics, seconds, 12 * TILE + 7, 24 * TILE - 4),
  );

  // Wall clock on the meeting room's top wall, left of the windows.
  const clockX = TILE + TILE / 2;
  const clockY = 7;
  place("clock", clockX - 8, clockY - 16, TILE + 2);
  addEffect(TILE + 3, (graphics, seconds) => {
    const minute = seconds * 0.4;
    const hour = minute / 12;
    graphics
      .moveTo(clockX, clockY)
      .lineTo(clockX + Math.sin(hour) * 2, clockY - Math.cos(hour) * 2)
      .stroke({ color: 0x1f2937, width: 1 });
    graphics
      .moveTo(clockX, clockY)
      .lineTo(clockX + Math.sin(minute) * 3.5, clockY - Math.cos(minute) * 3.5)
      .stroke({ color: 0x334155, width: 1 });
  });

  addEffect(TILE + TILE + 1, (graphics, seconds) => {
    const slides = [0x9fe3f5, 0xa8e6c2, 0xf2d3a0];
    const slide = slides[Math.floor(seconds / 5) % slides.length]!;
    const pulse = 0.12 + 0.1 * Math.sin(seconds * 2.2);
    graphics.rect(14 * TILE + 2, TILE + 3, 3 * TILE - 4, 9).fill({ color: slide, alpha: pulse });
  });

  addEffect(5 * TILE + TILE + 1, (graphics, seconds) => {
    const pulse = 0.08 + 0.06 * Math.sin(seconds * 3);
    graphics.rect(21 * TILE + 4, 5 * TILE - 24, 24, 13).fill({ color: 0x9fe3f5, alpha: pulse });
  });

  const drawRackLeds = (graphics: Graphics, seconds: number, x: number, y: number, w: number) => {
    for (let row = 0; row < 4; row++) {
      for (let dot = 0; dot < Math.min(6, w); dot++) {
        const beat = Math.floor(seconds * 2.4 + row * 1.7 + dot * 0.9) % 4;
        if (beat === 3) continue;
        graphics
          .rect(x * TILE + 5 + dot * 8, y * TILE - 17 + row * 6, 2, 2)
          .fill(dot % 2 === 0 ? 0x8ef0b0 : 0x9fe3f5);
      }
    }
  };
  addEffect(2 * TILE + TILE + 1, (graphics, seconds) => {
    drawRackLeds(graphics, seconds, 39, 2, 6);
    drawRackLeds(graphics, seconds, 46, 2, 6);
  });
  addEffect(9 * TILE + TILE + 1, (graphics, seconds) => {
    drawRackLeds(graphics, seconds, 39, 9, 6);
    drawRackLeds(graphics, seconds, 46, 9, 6);
  });

  addEffect(26 * TILE + TILE + 1, (graphics, seconds) => {
    if (Math.floor(seconds * 1.6) % 2 !== 0) return;
    graphics.rect(41 * TILE + 24, 26 * TILE - 9, 2, 2).fill(0x63e38a);
    graphics.rect(45 * TILE + 24, 26 * TILE - 9, 2, 2).fill(0x63e38a);
  });

  addEffect(27 * TILE - 1, (graphics, seconds) => {
    const warmth = 0.5 + 0.5 * Math.sin(seconds * 1.1);
    const cx = 8 * TILE + TILE / 2;
    const cy = 27 * TILE + TILE / 2;
    graphics.circle(cx, cy + 3, 16).fill({ color: 0xffd9a0, alpha: 0.05 + 0.04 * warmth });
    graphics.circle(cx, cy - 18, 7).fill({ color: 0xffe8bb, alpha: 0.12 + 0.08 * warmth });
  });

  addEffect(27 * TILE + TILE + 1, (graphics, seconds) => {
    const px = 13 * TILE;
    const py = 27 * TILE;
    for (let index = 0; index < 2; index++) {
      const cx = Math.round(px + 9 + Math.sin(seconds * 0.55 + index * 2.4) * 9);
      const cy = Math.round(py + 1 + index * 5 + Math.sin(seconds * 1.4 + index * 1.9) * 2);
      const color = index === 0 ? 0xe08a4f : 0x5aa8a2;
      graphics.rect(cx, cy, 4, 2).fill(color);
      graphics.rect(cx - 2, cy, 2, 2).fill(color);
      graphics.rect(cx + 3, cy, 1, 1).fill(OUTLINE);
    }
    for (let index = 0; index < 3; index++) {
      const phase = (seconds * 0.5 + index / 3) % 1;
      graphics
        .rect(px + 6 + index * 8, Math.round(py + 9 - phase * 10), 1, 1)
        .fill({ color: 0xffffff, alpha: 0.6 * (1 - phase) });
    }
  });

  // -- furniture ----------------------------------------------------------

  const plants: FurnitureSprite[] = ["plant", "plant2", "cactus"];

  const drawFurniture = (item: Furniture) => {
    const { x, y, w, h } = item.rect;
    const px = x * TILE;
    const py = y * TILE;
    const z = (y + h) * TILE;
    switch (item.kind) {
      case "desk": {
        worktop(x, y, w, z);
        // A few desks keep a mug beside the monitor.
        if (hashSeed(item.id) % 3 === 0) place("coffee", px + w * TILE - 14, py - 4, z + 0.5);
        break;
      }
      case "meeting-table":
        stretch("tableFront", [0, 11, 48, 53], [7, 13, 7, 8], px, py - 11, w * TILE, h * TILE + 11, z);
        for (const offset of [1, 4, 6]) place("coffee", px + offset * TILE, py - 6, z + 0.5);
        break;
      case "kitchen-table":
        stretch("smallTableFront", [0, 11, 32, 21], [2, 12, 2, 8], px, py - 11, w * TILE, h * TILE + 11, z);
        place("coffee", px + 4, py - 6, z + 0.5);
        place("coffee", px + 2 * TILE, py - 4, z + 0.5);
        break;
      case "counter":
        worktop(x, y, w, z);
        if (w === 2) place("pcSide", px + 8, py - 18, z + 0.5);
        break;
      case "reception-desk":
        worktop(x, y, w, z);
        place("pcSide", px + TILE, py - 18, z + 0.5);
        place("pot", px + (w - 1) * TILE - 2, py - 10, z + 0.5);
        break;
      case "whiteboard":
        stretch("whiteboard", [2, 8, 28, 16], [3, 3, 3, 3], px + 2, py - 4, w * TILE - 4, 16, z);
        break;
      case "sofa":
        // The north lounge sofa faces into the room; the rest face away.
        if (item.id === "lounge-sofa-b") {
          stretch("sofaBack", [0, 2, 32, 14], [6, 4, 6, 4], px, py + 2, w * TILE, 14, z);
        } else {
          stretch("sofaFront", [0, 0, 32, 16], [6, 6, 6, 4], px, py, w * TILE, 16, z);
        }
        break;
      case "coffee-table":
        stretch("coffeeTable", [2, 6, 28, 25], [6, 4, 6, 7], px, py - 4, w * TILE, 20, z);
        place("coffee", px + 4, py - 6, z + 0.5);
        break;
      case "bookshelf": {
        let cursor = px;
        for (let index = 0; index < Math.floor(w / 2); index++) {
          place("doubleBookshelf", cursor, py + TILE - 24, z);
          cursor += 2 * TILE;
        }
        if (w % 2 === 1) place("plant2", cursor, py + TILE - 31, z);
        break;
      }
      case "plant":
        place(plants[hashSeed(item.id) % plants.length]!, px, py + TILE - 31, z);
        break;
      case "stool":
        // Stools sit under whoever takes the seat.
        place("woodenBench", px, py, py + TILE - 3);
        break;
      case "chair":
        place("cushionedChairBack", px, py, py + TILE - 3);
        break;
      case "poster":
        place("largePainting", px, py - 12, z);
        break;
      case "screen":
        drawn(z, (graphics) => drawPresentationScreen(graphics, x, y, w));
        break;
      case "clock":
        // Hung on the top wall; see the wall clock above.
        break;
      case "coffee-machine":
        drawn(z + 0.5, (graphics) => drawCoffeeMachine(graphics, x, y));
        break;
      case "water-cooler":
        drawn(z, (graphics) => drawWaterCooler(graphics, x, y));
        break;
      case "fridge":
        drawn(z + 0.5, (graphics) => drawFridge(graphics, x, y));
        break;
      case "printer":
        drawn(z, (graphics) => drawPrinter(graphics, x, y));
        break;
      case "rack":
        drawn(z, (graphics) => drawRack(graphics, x, y, w));
        break;
      case "vending":
        drawn(z, (graphics) => drawVending(graphics, x, y));
        break;
      case "lamp":
        drawn(z, (graphics) => drawLamp(graphics, x, y));
        break;
      case "aquarium":
        drawn(z, (graphics) => drawAquarium(graphics, x, y, w));
        break;
      case "beanbag":
        drawn(z, (graphics) => drawBeanbag(graphics, x, y));
        break;
      case "window":
        drawn(z, (graphics) => drawWindow(graphics, px, py + 1, w * TILE));
        break;
    }
  };

  for (const item of layout.furniture) drawFurniture(item);

  // Desk monitors face the agent, so the camera sees their backs; the glow
  // spilling past the edges shows the screen is on.
  for (const agent of layout.agents) {
    const deskZ = (agent.desk.y + agent.desk.h) * TILE;
    // Off-centre and low on the desk, so the seated agent's face stays visible.
    const monitorX = agent.x - TILE + 1;
    const monitorY = agent.desk.y * TILE - 14;
    place("pcBack", monitorX, monitorY, deskZ + 0.5);
    const glow = new pixi.Graphics();
    glow.rect(monitorX - 1, monitorY + 5, TILE + 2, 1).fill(0x9fe3f5);
    glow.rect(monitorX - 1, monitorY + 5, 1, 12).fill(0x9fe3f5);
    glow.rect(monitorX + TILE, monitorY + 5, 1, 12).fill(0x9fe3f5);
    glow.alpha = 0;
    glow.zIndex = deskZ + 0.6;
    props.push(glow);
    screens.push({
      setOn(on, nowMs) {
        glow.alpha = on ? 0.35 + 0.35 * (0.5 + 0.5 * Math.sin(nowMs / 300)) : 0;
      },
    });

    // The chair sits behind its agent, who faces the camera.
    place("woodenChairFront", agent.x - TILE / 2, agent.y + 4 - 32, agent.y - 1);
  }

  // Meeting chairs: the north row faces the camera and sits behind its
  // occupant; the south row shows its backrest in front of them.
  for (const station of layout.stations) {
    if (station.kind !== "meeting") continue;
    if (station.dir === "up") {
      place("woodenChairBack", station.x - TILE / 2, station.y + 3 - 32, station.y + 1);
    } else {
      place("woodenChairFront", station.x - TILE / 2, station.y + 4 - 32, station.y - 1);
    }
  }

  return {
    floor,
    props,
    screens,
    update(nowMs: number) {
      const seconds = nowMs / 1000;
      for (const effect of effects) effect.draw(seconds);
    },
  };
}
