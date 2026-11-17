import type { Container, Graphics } from "pixi.js";
import type { OfficeDir } from "@multica/core/office";
import {
  feetAtTile,
  findPath,
  stepTowards,
  type OfficeLayout,
} from "./layout";

/**
 * The office cat. It wanders between rooms, naps in the lounge, and has no
 * collisions of its own: it never blocks a path or a station, so the game's
 * routing stays independent of where the cat happens to be.
 */

const CAT_SPEED = 34;
const NAP_MS = 9_000;
const SIT_MS = 4_000;
const MEOW_MS = 1_600;

const CAT_FUR = 0x6b7280;
const CAT_DARK = 0x4b5563;
const CAT_LIGHT = 0xd1d5db;
const CAT_EYE = 0x8ef0b0;

const SPOT_TILES = [
  [10, 16],
  [22, 8],
  [30, 29],
  [28, 32],
  [44, 6],
  [44, 31],
  [8, 9],
  [5, 30],
] as const;

type CatState = "walk" | "sit" | "sleep";

export class OfficeCat {
  private readonly view: Container;
  private readonly body: Graphics;
  private readonly tail: Graphics;
  private readonly bubbles: Graphics;
  private readonly spots: { x: number; y: number }[];
  private layout: OfficeLayout;
  private x: number;
  private y: number;
  private dir: OfficeDir = "left";
  private path: { x: number; y: number }[] = [];
  private state: CatState = "sit";
  private until = 0;
  private walkPhase = 0;
  private meowUntil = 0;
  private destroyed = false;

  constructor(
    pixi: typeof import("pixi.js"),
    layout: OfficeLayout,
    world: Container,
  ) {
    this.layout = layout;
    this.spots = SPOT_TILES.map(([tx, ty]) => feetAtTile(tx, ty));
    const start = this.spots[this.spots.length - 1]!;
    this.x = start.x;
    this.y = start.y;
    this.view = new pixi.Container();
    this.body = new pixi.Graphics();
    this.tail = new pixi.Graphics();
    this.bubbles = new pixi.Graphics();
    this.view.addChild(this.tail, this.body, this.bubbles);
    this.view.eventMode = "none";
    this.draw();
    this.place();
    world.addChild(this.view);
  }

  setLayout(layout: OfficeLayout) {
    this.layout = layout;
    this.path = [];
  }

  position(): { x: number; y: number } {
    return { x: this.x, y: this.y };
  }

  /** Picked by the random-event scheduler: look up and complain. */
  meow(nowMs: number) {
    if (this.destroyed) return;
    this.meowUntil = nowMs + MEOW_MS;
    this.draw();
  }

  /** A short dash across the room, used by the "cat crosses the office" beat. */
  scamper(nowMs: number) {
    if (this.destroyed) return;
    const target = this.spots[Math.floor(Math.random() * this.spots.length)]!;
    this.path = findPath(this.layout, { x: this.x, y: this.y }, target);
    if (this.path.length > 0) {
      this.state = "walk";
      this.until = nowMs;
    }
  }

  update(dt: number, nowMs: number) {
    if (this.destroyed) return;
    if (this.state === "walk") {
      if (this.path.length === 0) {
        this.state = "sit";
        this.until = nowMs + SIT_MS;
      } else {
        const waypoint = this.path[0]!;
        const step = stepTowards({ x: this.x, y: this.y }, waypoint, CAT_SPEED * dt);
        this.x = step.x;
        this.y = step.y;
        this.dir = step.dir ?? this.dir;
        this.walkPhase += dt * 9;
        if (step.arrived) this.path.shift();
      }
    } else if (nowMs >= this.until) {
      if (this.state === "sleep") {
        this.state = "walk";
        this.pickWalk();
      } else if (Math.random() < 0.35) {
        this.state = "sleep";
        this.until = nowMs + NAP_MS;
      } else {
        this.state = "walk";
        this.pickWalk();
      }
    }
    this.place();
    this.draw();
  }

  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    this.view.destroy({ children: true });
  }

  private pickWalk() {
    const target = this.spots[Math.floor(Math.random() * this.spots.length)]!;
    this.path = findPath(this.layout, { x: this.x, y: this.y }, target);
    if (this.path.length === 0) {
      this.state = "sit";
      this.until = performance.now() + SIT_MS;
    }
  }

  private place() {
    this.view.position.set(Math.round(this.x), Math.round(this.y));
    this.view.zIndex = Math.round(this.y);
  }

  private draw() {
    const sleeping = this.state === "sleep";
    const step = this.state === "walk" ? Math.sin(this.walkPhase) : 0;
    this.body.clear();
    this.tail.clear();
    this.bubbles.clear();

    // Tail: curls up when sitting, sways while walking.
    const tailX = this.dir === "left" ? 5 : -6;
    const tailY = sleeping ? -1 : -4 - Math.round(step);
    this.tail
      .rect(tailX, tailY, 2, 2)
      .fill(CAT_DARK)
      .rect(tailX + (this.dir === "left" ? 2 : -2), tailY - 2, 2, 2)
      .fill(CAT_DARK)
      .rect(tailX + (this.dir === "left" ? 4 : -4), tailY - 3, 2, 2)
      .fill(CAT_DARK);

    // Body.
    this.body.rect(-5, -5, 9, 5).fill(CAT_FUR);
    this.body.rect(-5, -6, 9, 1).fill(CAT_DARK);
    // Paws.
    this.body.rect(-4, -1 + Math.round(step), 2, 1).fill(CAT_LIGHT);
    this.body.rect(1, -1 - Math.round(step), 2, 1).fill(CAT_LIGHT);
    // Head and ears.
    this.body.rect(3, -9, 5, 5).fill(CAT_FUR);
    this.body.rect(3, -10, 1, 2).fill(CAT_DARK);
    this.body.rect(7, -10, 1, 2).fill(CAT_DARK);
    this.body.rect(4, -8, 1, 1).fill(sleeping ? CAT_DARK : CAT_EYE);
    this.body.rect(6, -8, 1, 1).fill(sleeping ? CAT_DARK : CAT_EYE);
    this.body.rect(5, -6, 1, 1).fill(0xe5a3a3);

    if (this.meowUntil > performance.now()) {
      this.bubbles.circle(6, -15, 3).fill({ color: 0xf8fafc, alpha: 0.92 });
      this.bubbles.rect(5, -16, 1, 1).fill(0x334155);
      this.bubbles.rect(7, -16, 1, 1).fill(0x334155);
      this.bubbles.rect(6, -14, 1, 1).fill(0x334155);
    }
  }
}
