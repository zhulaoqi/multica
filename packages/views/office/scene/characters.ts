import type { Container, Graphics, Sprite } from "pixi.js";
import type { OfficeDir } from "@multica/core/office";
import { hashSeed } from "./room";
import { CHARACTER_SHEETS, officeTextures, type CharacterRow } from "./textures";

export type AgentDeskStatus = "working" | "queued" | "idle" | "offline";

export const STATUS_COLORS: Record<AgentDeskStatus, number> = {
  working: 0x4f9bd9,
  queued: 0xd9a34f,
  idle: 0x9aa4ad,
  offline: 0x6b7280,
};

/** How a character stands, sits, or works at its station. */
export type CharacterPose =
  | "stand"
  | "sit"
  | "type"
  | "read"
  | "sleep"
  | "pour"
  | "drink"
  | "carry"
  | "wave";

/** Something a character holds while walking an errand. */
export type CharacterProp = "none" | "cup" | "paper";

/** Small speech balloon above the head. Drawn as pixels, never text. */
export type BubbleKind = "dots" | "alert" | "check" | "chat" | "sleep";

const BUBBLE_POP_MS = 180;
/** Bottom of the balloon tail, above the name label. */
const BUBBLE_BOTTOM = -43;

const WALK_FRAMES = [0, 1, 2, 1] as const;
const WALK_FRAME_MS = 150;
const TYPE_FRAMES = [3, 4] as const;
const TYPE_FRAME_MS = 300;
const READ_FRAMES = [5, 6] as const;
const READ_FRAME_MS = 600;
const SEATED_FRAME = 7;
const STAND_FRAME = 1;

/**
 * Seated frames are drawn higher in the sheet than walking ones; these offsets
 * drop each facing onto the chair so the hips meet the seat.
 */
const SEATED_OFFSET: Record<CharacterRow, number> = { 0: 4, 1: 5, 2: 2 };

// Pixel-agents balloon art: an 11 x 13 map, B = border, F = fill.
const BUBBLE_FRAME = [
  "_BBBBBBBBB_",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "BFFFFFFFFFB",
  "_BBBBBBBBB_",
  "____BBB____",
  "_____B_____",
] as const;
const BUBBLE_INK = { border: 0x555566, fill: 0xeeeeff } as const;

/** Icon pixels inside the balloon, as [column, row] pairs. */
const BUBBLE_ICONS: Record<BubbleKind, { color: number; pixels: [number, number][] }> = {
  dots: { color: 0x64748b, pixels: [[3, 5], [5, 5], [7, 5]] },
  chat: { color: 0x4f9bd9, pixels: [[3, 5], [5, 5], [7, 5]] },
  alert: { color: 0xcca700, pixels: [[5, 2], [5, 3], [5, 4], [5, 5], [5, 7]] },
  check: { color: 0x44bb66, pixels: [[8, 3], [7, 4], [3, 5], [6, 5], [4, 6], [5, 6]] },
  sleep: {
    color: 0x555566,
    pixels: [
      [3, 2], [4, 2], [5, 2], [6, 2], [7, 2],
      [6, 3], [5, 4], [4, 5],
      [3, 6], [4, 6], [5, 6], [6, 6], [7, 6],
    ],
  },
};

function paintBubble(graphics: Graphics, kind: BubbleKind) {
  graphics.clear();
  const left = -5;
  const top = -BUBBLE_FRAME.length;
  BUBBLE_FRAME.forEach((line, row) => {
    for (let col = 0; col < line.length; col++) {
      const cell = line[col];
      if (cell === "_") continue;
      graphics
        .rect(left + col, top + row, 1, 1)
        .fill(cell === "B" ? BUBBLE_INK.border : BUBBLE_INK.fill);
    }
  });
  const icon = BUBBLE_ICONS[kind];
  for (const [col, row] of icon.pixels) {
    graphics.rect(left + col, top + row, 1, 1).fill(icon.color);
  }
}

/** Mean time between blinks, matching pixel-agents' blink cadence. */
function blinkGap(focused: boolean): number {
  const gap = Math.min(1.5 - Math.log(1 - Math.random()) * 3.5, 11);
  return gap * (focused ? 1.5 : 1);
}

function rowFor(dir: OfficeDir): CharacterRow {
  if (dir === "down") return 0;
  if (dir === "up") return 1;
  return 2;
}

export interface CharacterView {
  container: Container;
  setWalking(walking: boolean): void;
  setDir(dir: OfficeDir | null): void;
  /** Agent availability; people do not use this. */
  setStatus(status: AgentDeskStatus): void;
  setPose(pose: CharacterPose): void;
  setProp(prop: CharacterProp): void;
  setBubble(kind: BubbleKind | null): void;
  /** Advances frames, blinks, and balloons; call once per rendered frame. */
  tick(timeMs: number): void;
}

interface CharacterOptions {
  kind: "human" | "agent";
  seed: string;
  /** Pins a sheet index instead of hashing the seed. */
  sheet?: number;
  status?: AgentDeskStatus;
}

/**
 * A 16 x 32 sprite character from the pixel-agents sheets: walk, type, read,
 * and seated frames per facing, plus a closed-eye sheet for blinking. The
 * sprite is anchored on its feet, so the container position is the floor spot.
 */
export function createCharacter(
  pixi: typeof import("pixi.js"),
  options: CharacterOptions,
): CharacterView {
  const textures = officeTextures();
  const sheet = textures.characters[(options.sheet ?? hashSeed(options.seed)) % CHARACTER_SHEETS]!;

  const container = new pixi.Container();
  // Only the rig mirrors when the character faces left. Labels and balloons
  // stay on the container so they never render backwards.
  const rig = new pixi.Container();
  container.addChild(rig);

  const shadow = new pixi.Graphics();
  shadow.rect(-5, -1, 10, 2).fill({ color: 0x000000, alpha: 0.2 });
  rig.addChild(shadow);

  const sprite: Sprite = new pixi.Sprite(sheet[0]![0]![STAND_FRAME]!);
  sprite.anchor.set(0.5, 1);
  sprite.y = 1;
  rig.addChild(sprite);

  // Held items hang off the leading hand so pouring and carrying read clearly.
  const prop = new pixi.Graphics();
  prop.visible = false;
  rig.addChild(prop);

  const statusDot = new pixi.Graphics();
  const paintStatus = (status: AgentDeskStatus) => {
    statusDot.clear();
    statusDot.rect(4, -31, 5, 5).fill(0x1f2430);
    statusDot.rect(5, -30, 3, 3).fill(STATUS_COLORS[status]);
  };
  if (options.status) {
    paintStatus(options.status);
    container.addChild(statusDot);
  }

  const bubble = new pixi.Container();
  const bubbleArt = new pixi.Graphics();
  bubble.addChild(bubbleArt);
  bubble.position.set(0, BUBBLE_BOTTOM);
  bubble.visible = false;
  container.addChild(bubble);

  let walking = false;
  let pose: CharacterPose = "stand";
  let row: CharacterRow = 0;
  let propKind: CharacterProp = "none";
  let bubbleKind: BubbleKind | null = null;
  let bubbleStartedAt = 0;
  let bubblePopped = false;

  let lastTickMs: number | null = null;
  let blinkIn = Math.random() * blinkGap(false);
  let blinkShut = 0;

  const seated = () => pose === "sit" || pose === "type" || pose === "read" || pose === "sleep";

  const paintProp = () => {
    prop.clear();
    // Facing away, the hands are hidden behind the body.
    if (propKind === "none" || row === 1) {
      prop.visible = false;
      return;
    }
    prop.visible = true;
    const x = row === 0 ? 4 : 5;
    const y = seated() ? -10 : -13;
    if (propKind === "cup") {
      prop.rect(x, y, 4, 4).fill(0x1f2430);
      prop.rect(x + 1, y + 1, 2, 2).fill(0xf8fafb);
      prop.rect(x + 1, y, 2, 1).fill(0x6b4426);
    } else {
      prop.rect(x - 1, y - 1, 6, 5).fill(0x1f2430);
      prop.rect(x, y, 4, 3).fill(0xf8fafb);
      prop.rect(x + 1, y + 1, 2, 1).fill(0x94a3b8);
    }
  };

  const frameFor = (timeMs: number): number => {
    if (walking) return WALK_FRAMES[Math.floor(timeMs / WALK_FRAME_MS) % WALK_FRAMES.length]!;
    switch (pose) {
      case "type":
        return TYPE_FRAMES[Math.floor(timeMs / TYPE_FRAME_MS) % TYPE_FRAMES.length]!;
      case "read":
        return READ_FRAMES[Math.floor(timeMs / READ_FRAME_MS) % READ_FRAMES.length]!;
      case "sit":
      case "sleep":
        return SEATED_FRAME;
      default:
        return STAND_FRAME;
    }
  };

  /** Advances the blink timer; true while the eyes are shut. */
  const blink = (dtSeconds: number): boolean => {
    if (blinkShut > 0) {
      blinkShut -= dtSeconds;
      return blinkShut > 0;
    }
    blinkIn -= dtSeconds;
    if (blinkIn > 0) return false;
    blinkShut = 0.1 + Math.random() * 0.07;
    blinkIn = Math.random() < 0.15 ? 0.15 + Math.random() * 0.15 : blinkGap(pose === "type");
    return true;
  };

  const view: CharacterView = {
    container,
    setWalking(next) {
      walking = next;
    },
    setDir(dir) {
      if (!dir) return;
      const nextRow = rowFor(dir);
      rig.scale.x = dir === "left" ? -1 : 1;
      if (nextRow !== row) {
        row = nextRow;
        paintProp();
      }
    },
    setStatus(status) {
      paintStatus(status);
      if (!statusDot.parent) container.addChild(statusDot);
    },
    setPose(next) {
      if (next === pose) return;
      pose = next;
      paintProp();
    },
    setProp(next) {
      if (next === propKind) return;
      propKind = next;
      paintProp();
    },
    setBubble(kind) {
      if (kind === bubbleKind) return;
      bubbleKind = kind;
      if (!kind) {
        bubble.visible = false;
        return;
      }
      paintBubble(bubbleArt, kind);
      bubble.visible = true;
      bubblePopped = false;
      bubbleStartedAt = performance.now();
    },
    tick(timeMs) {
      const dt = lastTickMs === null ? 0 : Math.max(0, (timeMs - lastTickMs) / 1000);
      lastTickMs = timeMs;

      const shut = pose === "sleep" || blink(dt);
      const sitting = seated() && !walking;
      sprite.texture = sheet[shut ? 1 : 0]![row]![frameFor(timeMs)]!;

      let lift = 0;
      if (!walking && pose === "pour") lift = Math.floor(timeMs / 440) % 2;
      else if (!walking && pose === "wave") lift = Math.floor(timeMs / 260) % 2;
      sprite.y = 1 + (sitting ? SEATED_OFFSET[row] : 0) - lift;
      shadow.visible = !sitting;
      prop.y = walking ? -(Math.floor(timeMs / WALK_FRAME_MS) % 2) : pose === "drink" ? -3 : 0;

      if (bubble.visible && !bubblePopped) {
        const elapsed = timeMs - bubbleStartedAt;
        if (elapsed >= BUBBLE_POP_MS) {
          bubblePopped = true;
          bubble.alpha = 1;
          bubble.scale.set(1);
        } else {
          const progress = Math.max(0, elapsed) / BUBBLE_POP_MS;
          bubble.alpha = progress;
          bubble.scale.set(0.6 + 0.4 * progress);
        }
      }
    },
  };

  return view;
}
