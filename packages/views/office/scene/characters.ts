import type { Container } from "pixi.js";
import type { OfficeDir } from "@multica/core/office";
import { characterColors, hashSeed } from "./room";

export type AgentDeskStatus = "working" | "queued" | "idle" | "offline";

export const STATUS_COLORS: Record<AgentDeskStatus, number> = {
  working: 0x4f9bd9,
  queued: 0xd9a34f,
  idle: 0x9aa4ad,
  offline: 0x6b7280,
};

/** How a character stands, sits, or works at its station. */
export type CharacterPose = "stand" | "sit" | "type" | "read" | "sleep";

/** Small speech balloon above the head. Drawn, never text, so it stays pixel-crisp. */
export type BubbleKind = "dots" | "alert" | "check" | "chat" | "sleep";

const AGENT_SHELL = 0xe2e8ee;
const AGENT_HANDS = 0x94a3b8;
const BUBBLE_POP_MS = 180;
const BUBBLE_HEIGHT = 12;
const BUBBLE_TOP = -47;

export interface CharacterView {
  container: Container;
  setWalking(walking: boolean): void;
  setDir(dir: OfficeDir | null): void;
  /** Agent availability; people do not use this. */
  setStatus(status: AgentDeskStatus): void;
  setPose(pose: CharacterPose): void;
  setBubble(kind: BubbleKind | null): void;
  /** Animates limbs and balloons; call once per rendered frame. */
  tick(timeMs: number): void;
}

interface CharacterOptions {
  kind: "human" | "agent";
  seed: string;
  status?: AgentDeskStatus;
}

/**
 * A 16 x 26 px pixel character drawn from rectangles: legs, body, arms, head,
 * and a hair cap (people) or visor and antenna (agents). Poses reuse the same
 * rig: sitting hides the legs and lowers the torso onto the chair, typing
 * beats the arms, reading raises a booklet, and sleeping tilts the head.
 */
export function createCharacter(
  pixi: typeof import("pixi.js"),
  options: CharacterOptions,
): CharacterView {
  const seed = hashSeed(options.seed);
  const container = new pixi.Container();

  const shadow = new pixi.Graphics();
  shadow.rect(-6, -2, 12, 3).fill({ color: 0x2f2a26, alpha: 0.18 });
  container.addChild(shadow);

  const pants = options.kind === "agent" ? 0x4b5563 : 0x394050;
  const hand = options.kind === "agent" ? AGENT_HANDS : characterColors.skin(seed);
  const leftLeg = new pixi.Graphics();
  const rightLeg = new pixi.Graphics();
  leftLeg.rect(-4, -5, 3, 5).fill(pants);
  rightLeg.rect(1, -5, 3, 5).fill(pants);
  container.addChild(leftLeg, rightLeg);

  const upper = new pixi.Container();
  const shirt = characterColors.shirt(seed);

  const body = new pixi.Graphics();
  body.rect(-5, -14, 10, 9).fill(shirt);
  body.rect(-5, -14, 10, 2).fill({ color: 0xffffff, alpha: 0.18 });
  upper.addChild(body);

  const leftArm = new pixi.Graphics();
  const rightArm = new pixi.Graphics();
  leftArm.rect(-7, -13, 2, 5).fill(shirt);
  leftArm.rect(-7, -8, 2, 2).fill(hand);
  rightArm.rect(5, -13, 2, 5).fill(shirt);
  rightArm.rect(5, -8, 2, 2).fill(hand);
  upper.addChild(leftArm, rightArm);

  const head = new pixi.Graphics();
  if (options.kind === "agent") {
    head.rect(-4, -21, 8, 7).fill(AGENT_SHELL);
    head.rect(-3, -19, 6, 2).fill(0x38bdf8);
    head.rect(0, -25, 1, 4).fill(0x94a3b8);
    head.circle(0, -26, 1.5).fill(0xd9a34f);
  } else {
    head.rect(-4, -21, 8, 7).fill(characterColors.skin(seed));
    head.rect(-4, -23, 8, 3).fill(characterColors.hair(seed));
    head.rect(-5, -21, 1, 4).fill(characterColors.hair(seed));
    head.rect(4, -21, 1, 3).fill(characterColors.hair(seed));
  }
  upper.addChild(head);

  const book = new pixi.Graphics();
  book.rect(-4, -13, 8, 5).fill(0xf4f6f8);
  book.rect(-4, -13, 1, 5).fill(0xd0d7de);
  book.rect(-3, -11, 6, 1).fill({ color: 0x94a3b8, alpha: 0.7 });
  book.visible = false;
  upper.addChild(book);

  const statusDot = new pixi.Graphics();
  const paintStatus = (status: AgentDeskStatus) => {
    statusDot.clear();
    statusDot.circle(0, -28, 2).fill(STATUS_COLORS[status]);
    statusDot.circle(0, -28, 2).stroke({ color: 0xffffff, width: 0.5, alpha: 0.8 });
  };
  if (options.status) {
    paintStatus(options.status);
    upper.addChild(statusDot);
  }

  container.addChild(upper);

  const bubble = new pixi.Container();
  const bubbleBg = new pixi.Graphics();
  bubble.addChild(bubbleBg);
  bubble.visible = false;
  container.addChild(bubble);

  let walking = false;
  let pose: CharacterPose = "stand";
  let bubbleKind: BubbleKind | null = null;
  let bubbleStartedAt = 0;
  let bubblePopped = false;

  const seated = () => pose !== "stand";

  const paintBubble = (kind: BubbleKind) => {
    bubbleBg.clear();
    bubbleBg
      .roundRect(-9, BUBBLE_TOP, 18, BUBBLE_HEIGHT, 3)
      .fill({ color: 0xffffff, alpha: 0.96 })
      .stroke({ color: 0x1f2937, width: 1 });
    bubbleBg
      .moveTo(-2, BUBBLE_TOP + BUBBLE_HEIGHT)
      .lineTo(2, BUBBLE_TOP + BUBBLE_HEIGHT)
      .lineTo(0, BUBBLE_TOP + BUBBLE_HEIGHT + 3)
      .closePath()
      .fill({ color: 0xffffff });
    const centreY = BUBBLE_TOP + BUBBLE_HEIGHT / 2;
    if (kind === "dots" || kind === "chat") {
      const ink = kind === "chat" ? 0x4f9bd9 : 0x64748b;
      for (const offset of [-4, 0, 4]) {
        bubbleBg.circle(offset, centreY, 1.2).fill(ink);
      }
    } else if (kind === "alert") {
      bubbleBg.rect(-0.8, BUBBLE_TOP + 2.5, 1.6, 4.5).fill(0xd9a34f);
      bubbleBg.circle(0, BUBBLE_TOP + 9, 0.9).fill(0xd9a34f);
    } else if (kind === "check") {
      bubbleBg
        .moveTo(-4, centreY)
        .lineTo(-1, centreY + 3)
        .lineTo(4, centreY - 3)
        .stroke({ color: 0x16a34a, width: 1.6 });
    } else {
      // sleep: two stacked "z" glyphs, drawn as strokes so no font is needed
      bubbleBg
        .moveTo(-5, BUBBLE_TOP + 6)
        .lineTo(-2, BUBBLE_TOP + 6)
        .lineTo(-5, BUBBLE_TOP + 3)
        .lineTo(-2, BUBBLE_TOP + 3)
        .stroke({ color: 0x64748b, width: 1 });
      bubbleBg
        .moveTo(1, BUBBLE_TOP + 9)
        .lineTo(3.5, BUBBLE_TOP + 9)
        .lineTo(1, BUBBLE_TOP + 6.5)
        .lineTo(3.5, BUBBLE_TOP + 6.5)
        .stroke({ color: 0x94a3b8, width: 1 });
    }
  };

  const view: CharacterView = {
    container,
    setWalking(next) {
      walking = next;
      if (!walking) {
        leftLeg.y = 0;
        rightLeg.y = 0;
      }
    },
    setDir(dir) {
      if (dir === "left") container.scale.x = -1;
      else if (dir === "right") container.scale.x = 1;
    },
    setStatus(status) {
      paintStatus(status);
      if (!statusDot.parent) upper.addChild(statusDot);
    },
    setPose(next) {
      if (next === pose) return;
      pose = next;
      leftLeg.visible = !seated();
      rightLeg.visible = !seated();
      book.visible = pose === "read";
      leftArm.y = 0;
      rightArm.y = 0;
      head.x = pose === "sleep" ? 1 : 0;
      upper.y = seated() ? 3 : 0;
      leftArm.alpha = pose === "sleep" ? 0.6 : 1;
      rightArm.alpha = pose === "sleep" ? 0.6 : 1;
    },
    setBubble(kind) {
      if (kind === bubbleKind) return;
      bubbleKind = kind;
      if (!kind) {
        bubble.visible = false;
        return;
      }
      paintBubble(kind);
      bubble.visible = true;
      bubblePopped = false;
      bubbleStartedAt = performance.now();
    },
    tick(timeMs) {
      if (walking && !seated()) {
        const step = Math.floor(timeMs / 140) % 2;
        leftLeg.y = step === 0 ? 0 : -1;
        rightLeg.y = step === 0 ? -1 : 0;
        upper.y = step === 0 ? 0 : -1;
      } else if (walking) {
        upper.y = 3;
      } else if (!seated()) {
        // Idle breathing: one pixel every ~1.2 s keeps the pixel grid crisp.
        upper.y = Math.floor(timeMs / 1200) % 2 === 0 ? 0 : 0.5;
      } else {
        upper.y = 3;
      }

      if (pose === "type") {
        const beat = Math.floor(timeMs / 90) % 2;
        leftArm.y = beat === 0 ? 0 : -1;
        rightArm.y = beat === 0 ? -1 : 0;
      }

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
