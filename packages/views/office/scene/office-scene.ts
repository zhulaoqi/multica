import type {
  Application,
  Container,
  FederatedPointerEvent,
  Text as PixiText,
} from "pixi.js";
import type { OfficeDir, OfficePeer, OfficePosition } from "@multica/core/office";
import { OFFICE_WORLD } from "@multica/core/office";
import { createCharacter, type CharacterView } from "./characters";
import {
  buildOfficeLayout,
  directionFor,
  findPath,
  isWalkableWorld,
  stepTowards,
  type OfficeLayout,
} from "./layout";
import { OfficeAgents, type OfficeAgentView, type OfficeHuman } from "./agents";
import { drawRoom } from "./room";

const FRAME_INTERVAL_MS = 1000 / 30;
const WALK_SPEED = 92;
const PEER_SPEED = 110;
const PEER_LERP = 12;
const PEER_REPATH_DISTANCE = 8;
const HUMAN_CHAT_RADIUS = 20;
const HUMAN_CHAT_MS = 1100;
const MOVE_KEYS = ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "w", "a", "s", "d"];

export type { OfficeAgentView };

export interface OfficePeerView extends OfficePeer {
  /** Display name resolved from the member list. */
  name: string;
}

export interface OfficeSceneOptions {
  pixi: typeof import("pixi.js");
  host: HTMLElement;
  zoom: number;
  localName: string;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  onAgentClick: (agentId: string) => void;
}

interface LocalAvatar {
  view: CharacterView;
  label: PixiText;
  x: number;
  y: number;
  dir: OfficeDir | null;
  path: { x: number; y: number }[];
  walking: boolean;
  lastReported: string;
  chatUntil: number;
}

interface PeerAvatar {
  view: CharacterView;
  label: PixiText;
  x: number;
  y: number;
  target: OfficePosition;
  path: { x: number; y: number }[];
  pathTarget: { x: number; y: number };
  chatUntil: number;
}

/**
 * Owns the Pixi application, the room, the avatars, and input.
 *
 * React hands it peers and agents; it never fetches. The only data flowing
 * back out is the local position (throttled by the room hook) and agent
 * clicks. Agents animate themselves through OfficeAgents.
 */
export class OfficeScene {
  private readonly app: Application;
  private readonly pixi: typeof import("pixi.js");
  private readonly layout: OfficeLayout;
  private readonly world: Container;
  private readonly overlay: Container;
  private readonly local: LocalAvatar;
  private readonly peers = new Map<string, PeerAvatar>();
  private readonly agents: OfficeAgents;
  private zoom: number;
  private accumulator = 0;
  private keys = new Set<string>();
  private destroyed = false;
  private readonly onLocalMove: (position: OfficePosition) => void;

  private constructor(
    pixi: typeof import("pixi.js"),
    app: Application,
    world: Container,
    overlay: Container,
    options: OfficeSceneOptions,
    layout: OfficeLayout,
  ) {
    this.pixi = pixi;
    this.app = app;
    this.world = world;
    this.overlay = overlay;
    this.layout = layout;
    this.zoom = options.zoom;
    this.onLocalMove = options.onLocalMove;

    this.local = {
      view: createCharacter(pixi, { kind: "human", seed: "local-player" }),
      label: this.createLabel(options.localName),
      x: options.spawn.x,
      y: options.spawn.y,
      dir: "down",
      path: [],
      walking: false,
      lastReported: "",
      chatUntil: 0,
    };
    this.applyLocalPosition();

    this.agents = new OfficeAgents({
      pixi,
      layout,
      world,
      createLabel: (text) => this.createLabel(text),
      onAgentClick: options.onAgentClick,
    });
  }

  static async create(options: OfficeSceneOptions): Promise<OfficeScene> {
    const { pixi } = options;
    const app = new pixi.Application();
    await app.init({
      width: Math.round(OFFICE_WORLD.width * options.zoom),
      height: Math.round(OFFICE_WORLD.height * options.zoom),
      backgroundAlpha: 0,
      antialias: false,
      roundPixels: true,
      autoDensity: true,
      resolution: Math.min(window.devicePixelRatio || 1, 2),
      preference: "webgl",
    });
    app.canvas.style.display = "block";
    app.canvas.style.imageRendering = "pixelated";
    options.host.appendChild(app.canvas);

    const world = new pixi.Container();
    const overlay = new pixi.Container();
    const layout = buildOfficeLayout();
    world.addChild(drawRoom(pixi, layout));
    world.scale.set(options.zoom);
    app.stage.addChild(world, overlay);

    const scene = new OfficeScene(pixi, app, world, overlay, options, layout);
    scene.world.addChild(scene.local.view.container);
    scene.attachInput();
    app.ticker.add(scene.update);
    return scene;
  }

  setPeers(peers: OfficePeerView[]) {
    if (this.destroyed) return;
    const present = new Set<string>();
    for (const peer of peers) {
      present.add(peer.userId);
      const existing = this.peers.get(peer.userId);
      if (existing) {
        existing.target = { x: peer.x, y: peer.y, dir: peer.dir };
        if (existing.label.text !== peer.name) existing.label.text = peer.name;
        continue;
      }
      const view = createCharacter(this.pixi, { kind: "human", seed: peer.userId });
      view.container.position.set(peer.x, peer.y);
      this.world.addChild(view.container);
      this.peers.set(peer.userId, {
        view,
        label: this.createLabel(peer.name),
        x: peer.x,
        y: peer.y,
        target: { x: peer.x, y: peer.y, dir: peer.dir },
        path: [],
        pathTarget: { x: peer.x, y: peer.y },
        chatUntil: 0,
      });
    }
    for (const [userId, peer] of this.peers) {
      if (present.has(userId)) continue;
      peer.view.container.destroy({ children: true });
      peer.label.destroy();
      this.peers.delete(userId);
    }
  }

  setAgents(agents: OfficeAgentView[]) {
    if (this.destroyed) return;
    this.agents.setAgents(agents);
    this.agents.syncLabels(this.zoom);
  }

  setZoom(zoom: number) {
    if (this.destroyed || zoom === this.zoom) return;
    this.zoom = zoom;
    this.world.scale.set(zoom);
    this.app.renderer.resize(
      Math.round(OFFICE_WORLD.width * zoom),
      Math.round(OFFICE_WORLD.height * zoom),
    );
    this.syncLabels();
  }

  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    this.detachInput();
    this.app.ticker.remove(this.update);
    this.agents.destroy();
    try {
      this.app.destroy({ removeView: true }, { children: true });
    } catch {
      // The renderer may already be gone after a hot reload; nothing to do.
    }
  }

  private createLabel(text: string): PixiText {
    const label = new this.pixi.Text({
      text,
      style: {
        fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
        fontSize: 10,
        fill: 0xffffff,
        stroke: { color: 0x111827, width: 3 },
      },
    });
    label.anchor.set(0.5, 1);
    label.resolution = 2;
    this.overlay.addChild(label);
    return label;
  }

  private update = (ticker: { deltaMS: number }) => {
    if (this.destroyed) return;
    this.accumulator += ticker.deltaMS;
    if (this.accumulator < FRAME_INTERVAL_MS) return;
    const dt = Math.min(this.accumulator / 1000, 0.2);
    this.accumulator = 0;

    const now = performance.now();
    this.stepLocal(dt);
    this.stepPeers(dt);
    this.stepHumanChat(now);

    const humans: OfficeHuman[] = [
      { x: this.local.x, y: this.local.y },
      ...Array.from(this.peers.values(), (peer) => ({ x: peer.x, y: peer.y })),
    ];
    this.agents.update(dt, now, humans);

    this.local.view.tick(now);
    for (const peer of this.peers.values()) peer.view.tick(now);
    this.syncLabels();
  };

  /** Humans standing next to each other trade chat balloons. */
  private stepHumanChat(now: number) {
    let localChatting = false;
    for (const peer of this.peers.values()) {
      const distance = Math.hypot(peer.x - this.local.x, peer.y - this.local.y);
      if (distance >= HUMAN_CHAT_RADIUS) continue;
      localChatting = true;
      peer.chatUntil = now + HUMAN_CHAT_MS;
    }
    if (localChatting) this.local.chatUntil = now + HUMAN_CHAT_MS;
    this.local.view.setBubble(now < this.local.chatUntil ? "chat" : null);
    for (const peer of this.peers.values()) {
      peer.view.setBubble(now < peer.chatUntil ? "chat" : null);
    }
  }

  private readInput(): { dx: number; dy: number } {
    let dx = 0;
    let dy = 0;
    if (this.keys.has("ArrowUp") || this.keys.has("w")) dy -= 1;
    if (this.keys.has("ArrowDown") || this.keys.has("s")) dy += 1;
    if (this.keys.has("ArrowLeft") || this.keys.has("a")) dx -= 1;
    if (this.keys.has("ArrowRight") || this.keys.has("d")) dx += 1;
    return { dx, dy };
  }

  private stepLocal(dt: number) {
    const input = this.readInput();
    let moved = false;

    if (input.dx !== 0 || input.dy !== 0) {
      this.local.path = [];
      const length = Math.hypot(input.dx, input.dy);
      const stepX = (input.dx / length) * WALK_SPEED * dt;
      const stepY = (input.dy / length) * WALK_SPEED * dt;
      const nextX = this.local.x + stepX;
      if (isWalkableWorld(this.layout, nextX, this.local.y)) {
        this.local.x = nextX;
        moved = true;
      }
      const nextY = this.local.y + stepY;
      if (isWalkableWorld(this.layout, this.local.x, nextY)) {
        this.local.y = nextY;
        moved = true;
      }
      this.local.dir = directionFor(input.dx, input.dy);
    } else if (this.local.path.length > 0) {
      const waypoint = this.local.path[0]!;
      const step = stepTowards(
        { x: this.local.x, y: this.local.y },
        waypoint,
        WALK_SPEED * dt,
      );
      this.local.x = step.x;
      this.local.y = step.y;
      this.local.dir = step.dir ?? this.local.dir;
      if (step.arrived) this.local.path.shift();
      moved = true;
    }

    this.applyLocalPosition();
    this.local.view.setWalking(moved);
    const reported = `${Math.round(this.local.x)}:${Math.round(this.local.y)}:${this.local.dir ?? ""}`;
    if (moved && reported !== this.local.lastReported) {
      this.local.lastReported = reported;
      this.onLocalMove({
        x: Math.round(this.local.x),
        y: Math.round(this.local.y),
        dir: this.local.dir,
      });
    }
  }

  private applyLocalPosition() {
    this.local.view.container.position.set(
      Math.round(this.local.x),
      Math.round(this.local.y),
    );
    this.local.view.setDir(this.local.dir);
  }

  private stepPeers(dt: number) {
    for (const peer of this.peers.values()) {
      if (
        Math.hypot(
          peer.target.x - peer.pathTarget.x,
          peer.target.y - peer.pathTarget.y,
        ) > PEER_REPATH_DISTANCE
      ) {
        peer.path = findPath(
          this.layout,
          { x: peer.x, y: peer.y },
          { x: peer.target.x, y: peer.target.y },
        );
        peer.pathTarget = { x: peer.target.x, y: peer.target.y };
      }

      let remaining = PEER_SPEED * dt;
      let moved = false;
      while (remaining > 0 && peer.path.length > 0) {
        const waypoint = peer.path[0]!;
        const distance = Math.hypot(waypoint.x - peer.x, waypoint.y - peer.y);
        const step = stepTowards({ x: peer.x, y: peer.y }, waypoint, remaining);
        peer.x = step.x;
        peer.y = step.y;
        if (step.dir) peer.view.setDir(step.dir);
        moved = true;
        if (step.arrived) {
          peer.path.shift();
          remaining -= distance;
        } else {
          remaining = 0;
        }
      }

      if (peer.path.length === 0) {
        const distance = Math.hypot(peer.target.x - peer.x, peer.target.y - peer.y);
        if (distance > 0.5) {
          // Smooth the last stretch so a peer restarting does not snap.
          const factor = Math.min(1, dt * PEER_LERP);
          peer.x += (peer.target.x - peer.x) * factor;
          peer.y += (peer.target.y - peer.y) * factor;
          peer.view.setDir(directionFor(peer.target.x - peer.x, peer.target.y - peer.y));
          moved = true;
        } else {
          peer.x = peer.target.x;
          peer.y = peer.target.y;
          peer.view.setDir(peer.target.dir);
        }
      }

      peer.view.container.position.set(Math.round(peer.x), Math.round(peer.y));
      peer.view.setWalking(moved);
    }
  }

  private syncLabels() {
    const zoom = this.zoom;
    this.local.label.position.set(
      Math.round(this.local.view.container.x * zoom),
      Math.round(this.local.view.container.y * zoom) + 8,
    );
    for (const peer of this.peers.values()) {
      peer.label.position.set(
        Math.round(peer.view.container.x * zoom),
        Math.round(peer.view.container.y * zoom) + 8,
      );
    }
    this.agents.syncLabels(zoom);
  }

  private attachInput() {
    const stage = this.app.stage;
    stage.eventMode = "static";
    stage.hitArea = new this.pixi.Rectangle(
      0,
      0,
      Math.round(OFFICE_WORLD.width * this.zoom),
      Math.round(OFFICE_WORLD.height * this.zoom),
    );
    stage.on("pointerdown", this.handleStagePointerDown);
    window.addEventListener("keydown", this.handleKeyDown);
    window.addEventListener("keyup", this.handleKeyUp);
    window.addEventListener("blur", this.handleBlur);
  }

  private detachInput() {
    this.app.stage?.off("pointerdown", this.handleStagePointerDown);
    window.removeEventListener("keydown", this.handleKeyDown);
    window.removeEventListener("keyup", this.handleKeyUp);
    window.removeEventListener("blur", this.handleBlur);
  }

  private handleStagePointerDown = (event: FederatedPointerEvent) => {
    const point = this.world.toLocal(event.global);
    const path = findPath(
      this.layout,
      { x: this.local.x, y: this.local.y },
      point,
    );
    if (path.length > 0) this.local.path = path;
  };

  private handleKeyDown = (event: KeyboardEvent) => {
    if (isTypingTarget(event.target)) return;
    const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
    if (!MOVE_KEYS.includes(key)) return;
    event.preventDefault();
    this.keys.add(key);
  };

  private handleKeyUp = (event: KeyboardEvent) => {
    const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
    this.keys.delete(key);
  };

  private handleBlur = () => {
    this.keys.clear();
  };
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}
