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

const AGENT_SHELL = 0xe2e8ee;

export interface CharacterView {
  container: Container;
  setWalking(walking: boolean): void;
  setDir(dir: OfficeDir | null): void;
  /** Agent availability; people do not use this. */
  setStatus(status: AgentDeskStatus): void;
  /** Animates the legs; call once per rendered frame. */
  tick(timeMs: number): void;
}

interface CharacterOptions {
  kind: "human" | "agent";
  seed: string;
  status?: AgentDeskStatus;
}

/**
 * A 16 x 26 px pixel character drawn from rectangles: legs, body, head, and a
 * hair cap (people) or visor and antenna (agents). Faces left by flipping the
 * whole container.
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

  const leftLeg = new pixi.Graphics();
  const rightLeg = new pixi.Graphics();
  const pants = options.kind === "agent" ? 0x4b5563 : 0x394050;
  leftLeg.rect(-4, -5, 3, 5).fill(pants);
  rightLeg.rect(1, -5, 3, 5).fill(pants);
  container.addChild(leftLeg, rightLeg);

  const body = new pixi.Graphics();
  body.rect(-5, -14, 10, 9).fill(characterColors.shirt(seed));
  body.rect(-5, -14, 10, 2).fill({ color: 0xffffff, alpha: 0.18 });
  container.addChild(body);

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
  container.addChild(head);

  const statusDot = new pixi.Graphics();
  const paintStatus = (status: AgentDeskStatus) => {
    statusDot.clear();
    statusDot.circle(0, -28, 2).fill(STATUS_COLORS[status]);
    statusDot.circle(0, -28, 2).stroke({ color: 0xffffff, width: 0.5, alpha: 0.8 });
  };
  if (options.status) {
    paintStatus(options.status);
    container.addChild(statusDot);
  }

  let walking = false;

  const view: CharacterView = {
    container,
    setWalking(next) {
      walking = next;
      if (!walking) {
        leftLeg.y = 0;
        rightLeg.y = 0;
        body.y = 0;
      }
    },
    setDir(dir) {
      if (dir === "left") container.scale.x = -1;
      else if (dir === "right") container.scale.x = 1;
    },
    setStatus(status) {
      paintStatus(status);
      if (!statusDot.parent) container.addChild(statusDot);
    },
    tick(timeMs) {
      if (!walking) return;
      const step = Math.floor(timeMs / 140) % 2;
      leftLeg.y = step === 0 ? 0 : -1;
      rightLeg.y = step === 0 ? -1 : 0;
      body.y = step === 0 ? 0 : -1;
    },
  };

  return view;
}
