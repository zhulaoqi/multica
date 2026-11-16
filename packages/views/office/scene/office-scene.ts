import type {
  Application,
  Container,
  FederatedPointerEvent,
  Text as PixiText,
} from "pixi.js";
import type { OfficeDir, OfficePeer, OfficePosition } from "@multica/core/office";
import { OFFICE_WORLD } from "@multica/core/office";
import { createCharacter, type BubbleKind, type CharacterView } from "./characters";
import {
  buildOfficeLayout,
  directionFor,
  feetAtTile,
  findPath,
  isWalkableWorld,
  stepTowards,
  type OfficeLayout,
  type RoomId,
} from "./layout";
import { OfficeAgents, type OfficeAgentView, type OfficeHuman } from "./agents";
import { nearestAgent, nearestVisitSlot } from "./interactions";
import { isTypingTarget } from "./input";
import { buildRoom, type RoomView } from "./room";
import { loadOfficeTextures } from "./textures";

const FRAME_INTERVAL_MS = 1000 / 30;
const WALK_SPEED = 96;
const PEER_SPEED = 110;
const PEER_LERP = 12;
const PEER_REPATH_DISTANCE = 8;
const HUMAN_CHAT_RADIUS = 20;
const HUMAN_CHAT_MS = 1100;
/** How close the player must stand to see the interact hint on an agent. */
const FOCUS_RADIUS = 34;
/** Standing this close counts as "at the agent", so the panel opens now. */
const ENGAGE_RADIUS = 52;
const MIN_ZOOM = 1.25;
const MAX_ZOOM = 3;
const CAMERA_LERP = 6;
/** Room names sit above everything so they stay readable as people walk by. */
const ROOM_LABEL_Z = 1_000_000;
/** Floor row just behind the reception desk (desk spans x 22..27 on row 27). */
const RECEPTION_STAFF_ROW = 26;
/** Sheets 1 and 7 are the long-haired characters in dresses. */
const RECEPTIONISTS = [
  { sheet: 1, tx: 23 },
  { sheet: 7, tx: 24 },
] as const;
const MOVE_KEYS = ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "w", "a", "s", "d"];

export type { OfficeAgentView };

export interface OfficePeerView extends OfficePeer {
  /** Display name resolved from the member list. */
  name: string;
}

export interface OfficeSceneOptions {
  pixi: typeof import("pixi.js");
  host: HTMLElement;
  width: number;
  height: number;
  localName: string;
  /** Display name per room, drawn in each room's top-left corner. */
  roomNames: Record<RoomId, string>;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  /** The player reached an agent and wants to talk to it. */
  onAgentActivate: (agentId: string) => void;
  /** Nearest agent to the player changed, so the HUD can hint at interact. */
  onFocusAgentChange: (agentId: string | null) => void;
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
 * Owns the Pixi application, the room, the avatars, the camera, and input.
 *
 * React hands it peers and agents; it never fetches. The only data flowing
 * back out is the local position (throttled by the room hook), agent
 * activation, and the focused agent for the interact hint. Agents animate
 * themselves through OfficeAgents.
 */
export class OfficeScene {
  private readonly app: Application;
  private readonly pixi: typeof import("pixi.js");
  private readonly layout: OfficeLayout;
  private readonly camera: Container;
  private readonly world: Container;
  private readonly room: RoomView;
  private readonly local: LocalAvatar;
  private readonly peers = new Map<string, PeerAvatar>();
  private readonly receptionists: CharacterView[] = [];
  private readonly agents: OfficeAgents;
  private zoom = 2;
  private camX = 0;
  private camY = 0;
  private accumulator = 0;
  private keys = new Set<string>();
  private destroyed = false;
  private readonly onLocalMove: (position: OfficePosition) => void;
  private readonly onAgentActivate: (agentId: string) => void;
  private readonly onFocusAgentChange: (agentId: string | null) => void;
  private focusedAgentId: string | null = null;
  /** Agent the player is walking toward; activated once close enough. */
  private pendingAgentId: string | null = null;

  private constructor(
    pixi: typeof import("pixi.js"),
    app: Application,
    camera: Container,
    world: Container,
    room: RoomView,
    options: OfficeSceneOptions,
    layout: OfficeLayout,
  ) {
    this.pixi = pixi;
    this.app = app;
    this.camera = camera;
    this.world = world;
    this.room = room;
    this.layout = layout;
    this.onLocalMove = options.onLocalMove;
    this.onAgentActivate = options.onAgentActivate;
    this.onFocusAgentChange = options.onFocusAgentChange;

    this.local = {
      view: createCharacter(pixi, { kind: "human", seed: "local-player" }),
      label: this.createLabel(options.localName),
      x: options.spawn.x,
      y: options.spawn.y,
      dir: "up",
      path: [],
      walking: false,
      lastReported: "",
      chatUntil: 0,
    };
    this.local.view.container.addChild(this.local.label);
    this.local.label.position.set(0, -42);
    this.local.label.style.fontSize = 8.5;
    this.world.addChild(this.local.view.container);
    this.applyLocalPosition();

    this.agents = new OfficeAgents({
      pixi,
      layout,
      world,
      screens: room.screens,
      createLabel: (text) => this.createLabel(text),
      onAgentActivate: (agentId) => this.activateAgent(agentId),
    });
  }

  static async create(options: OfficeSceneOptions): Promise<OfficeScene> {
    const { pixi } = options;
    const app = new pixi.Application();
    await app.init({
      width: Math.max(1, options.width),
      height: Math.max(1, options.height),
      backgroundAlpha: 0,
      antialias: false,
      roundPixels: true,
      autoDensity: true,
      resolution: Math.min(window.devicePixelRatio || 1, 2),
      preference: "webgl",
    });
    app.canvas.style.display = "block";
    app.canvas.style.imageRendering = "pixelated";
    app.canvas.style.width = "100%";
    app.canvas.style.height = "100%";
    options.host.appendChild(app.canvas);

    const camera = new pixi.Container();
    const world = new pixi.Container();
    world.sortableChildren = true;
    camera.addChild(world);
    app.stage.addChild(camera);

    await loadOfficeTextures(pixi);
    const layout = buildOfficeLayout();
    const room = buildRoom(pixi, layout);
    room.floor.zIndex = -1000;
    world.addChild(room.floor);
    for (const prop of room.props) world.addChild(prop);

    const scene = new OfficeScene(pixi, app, camera, world, room, options, layout);
    scene.addRoomLabels(options.roomNames);
    scene.addReceptionists();
    scene.attachInput();
    scene.updateCamera(1, true);
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
      const label = this.createLabel(peer.name);
      label.position.set(0, -42);
      view.container.addChild(label);
      this.peers.set(peer.userId, {
        view,
        label,
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
      this.peers.delete(userId);
    }
  }

  setAgents(agents: OfficeAgentView[]) {
    if (this.destroyed) return;
    this.agents.setAgents(agents);
  }

  /** The wrapper resized; keep the same world area in view. */
  resize(width: number, height: number) {
    if (this.destroyed) return;
    this.app.renderer.resize(Math.max(1, width), Math.max(1, height));
    this.app.stage.hitArea = new this.pixi.Rectangle(0, 0, width, height);
    this.updateCamera(1, true);
  }

  zoomBy(delta: number) {
    const next = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, this.zoom + delta));
    if (next === this.zoom) return;
    this.zoom = next;
    this.updateCamera(1, true);
  }

  /**
   * Walks the player to an agent and opens the interaction panel once they are
   * there. If the agent is already close the panel opens immediately; if there
   * is no reachable side to stand on, it opens without the walk.
   */
  activateAgent(agentId: string) {
    if (this.destroyed) return;
    const agent = this.agents.positionOf(agentId);
    if (!agent) {
      this.onAgentActivate(agentId);
      return;
    }

    const distance = Math.hypot(agent.x - this.local.x, agent.y - this.local.y);
    if (distance <= ENGAGE_RADIUS) {
      this.pendingAgentId = null;
      this.local.path = [];
      this.face(agent.x, agent.y);
      this.onAgentActivate(agentId);
      return;
    }

    const slot = nearestVisitSlot(this.layout, agent, {
      x: this.local.x,
      y: this.local.y,
    });
    if (!slot) {
      this.onAgentActivate(agentId);
      return;
    }

    this.pendingAgentId = agentId;
    this.local.path = findPath(this.layout, { x: this.local.x, y: this.local.y }, slot);
  }

  /** Pauses the agent's routine and turns it toward the player. */
  setEngagedAgent(agentId: string | null) {
    if (this.destroyed) return;
    this.agents.engage(agentId);
  }

  playEmote(agentId: string, kind: BubbleKind) {
    if (this.destroyed) return;
    this.agents.playEmote(agentId, kind, performance.now());
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

  // -- internals ---------------------------------------------------------

  private face(x: number, y: number) {
    this.local.dir =
      directionFor(x - this.local.x, y - this.local.y) ?? this.local.dir;
  }

  /**
   * Finishes a click-to-visit: opens the panel when close, and replans if the
   * idle agent wandered off while the player was walking over.
   */
  private stepPendingInteract() {
    const agentId = this.pendingAgentId;
    if (!agentId) return;

    const agent = this.agents.positionOf(agentId);
    if (!agent) {
      this.pendingAgentId = null;
      return;
    }

    const distance = Math.hypot(agent.x - this.local.x, agent.y - this.local.y);
    if (distance <= ENGAGE_RADIUS) {
      this.pendingAgentId = null;
      this.local.path = [];
      this.face(agent.x, agent.y);
      this.onAgentActivate(agentId);
      return;
    }

    // Still walking: keep the current path.
    if (this.local.path.length > 0) return;

    const slot = nearestVisitSlot(this.layout, agent, {
      x: this.local.x,
      y: this.local.y,
    });
    if (!slot) {
      this.pendingAgentId = null;
      return;
    }
    this.local.path = findPath(this.layout, { x: this.local.x, y: this.local.y }, slot);
  }

  /** Emits focus changes only, so the HUD does not re-render every frame. */
  private updateFocus() {
    const agent = nearestAgent(
      this.agents.positions(),
      { x: this.local.x, y: this.local.y },
      FOCUS_RADIUS,
    );
    const id = agent?.id ?? null;
    if (id === this.focusedAgentId) return;
    this.focusedAgentId = id;
    this.onFocusAgentChange(id);
  }

  private addRoomLabels(names: Record<RoomId, string>) {
    for (const room of this.layout.rooms) {
      const tag = new this.pixi.Container();
      const text = new this.pixi.Text({
        text: names[room.id],
        style: {
          fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
          fontSize: 8,
          fontWeight: "700",
          fill: 0xf8fafc,
        },
      });
      text.resolution = 4;
      text.position.set(4, 2);
      const plate = new this.pixi.Graphics();
      plate
        .roundRect(0, 0, Math.ceil(text.width) + 8, Math.ceil(text.height) + 4, 3)
        .fill({ color: 0x0f172a, alpha: 0.72 });
      tag.addChild(plate, text);
      tag.position.set(room.rect.x * OFFICE_WORLD.tile + 3, room.rect.y * OFFICE_WORLD.tile + 3);
      tag.zIndex = ROOM_LABEL_Z;
      tag.eventMode = "none";
      this.world.addChild(tag);
    }
  }

  /** Two front-desk staff stand behind the reception desk, clear of the agent station at (26, 26). */
  private addReceptionists() {
    for (const { sheet, tx } of RECEPTIONISTS) {
      const view = createCharacter(this.pixi, { kind: "human", seed: `receptionist-${sheet}`, sheet });
      const feet = feetAtTile(tx, RECEPTION_STAFF_ROW);
      view.container.position.set(feet.x, feet.y);
      view.container.zIndex = Math.round(feet.y);
      view.container.eventMode = "none";
      view.setDir("down");
      this.world.addChild(view.container);
      this.receptionists.push(view);
    }
  }

  private createLabel(text: string): PixiText {
    const label = new this.pixi.Text({
      text,
      style: {
        fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
        fontSize: 9,
        fontWeight: "600",
        fill: 0xf8fafc,
        stroke: { color: 0x0f172a, width: 2.5, join: "round" },
      },
    });
    label.anchor.set(0.5, 0);
    label.resolution = 2;
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
    this.stepPendingInteract();
    this.stepPeers(dt);
    this.stepHumanChat(now);

    const humans: OfficeHuman[] = [
      { x: this.local.x, y: this.local.y },
      ...Array.from(this.peers.values(), (peer) => ({ x: peer.x, y: peer.y })),
    ];
    this.agents.update(dt, now, humans);
    this.room.update(now);
    this.updateFocus();
    this.updateCamera(dt);

    this.local.view.tick(now);
    for (const peer of this.peers.values()) peer.view.tick(now);
    for (const view of this.receptionists) view.tick(now);
  };

  /** Keeps the player centred, clamped to the world edges. */
  private updateCamera(dt: number, snap = false) {
    const zoom = this.zoom;
    const viewW = this.app.screen.width / zoom;
    const viewH = this.app.screen.height / zoom;
    const clampAxis = (value: number, view: number, world: number) => {
      if (view >= world) return (world - view) / 2;
      return Math.min(Math.max(value, 0), world - view);
    };
    const targetX = clampAxis(this.local.x - viewW / 2, viewW, OFFICE_WORLD.width);
    const targetY = clampAxis(this.local.y - viewH / 2, viewH, OFFICE_WORLD.height);

    if (snap) {
      this.camX = targetX;
      this.camY = targetY;
    } else {
      const factor = Math.min(1, dt * CAMERA_LERP);
      this.camX += (targetX - this.camX) * factor;
      this.camY += (targetY - this.camY) * factor;
    }

    this.camera.scale.set(zoom);
    this.camera.position.set(
      Math.round(-this.camX * zoom),
      Math.round(-this.camY * zoom),
    );
  }

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
      // Taking over the controls cancels a click-to-visit.
      this.local.path = [];
      this.pendingAgentId = null;
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
    this.local.view.container.zIndex = Math.round(this.local.y);
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
      peer.view.container.zIndex = Math.round(peer.y);
      peer.view.setWalking(moved);
    }
  }

  private attachInput() {
    const stage = this.app.stage;
    stage.eventMode = "static";
    stage.hitArea = new this.pixi.Rectangle(
      0,
      0,
      this.app.screen.width,
      this.app.screen.height,
    );
    stage.on("pointerdown", this.handleStagePointerDown);
    this.app.canvas.addEventListener("wheel", this.handleWheel, { passive: false });
    window.addEventListener("keydown", this.handleKeyDown);
    window.addEventListener("keyup", this.handleKeyUp);
    window.addEventListener("blur", this.handleBlur);
  }

  private detachInput() {
    this.app.stage?.off("pointerdown", this.handleStagePointerDown);
    this.app.canvas.removeEventListener("wheel", this.handleWheel);
    window.removeEventListener("keydown", this.handleKeyDown);
    window.removeEventListener("keyup", this.handleKeyUp);
    window.removeEventListener("blur", this.handleBlur);
  }

  private handleWheel = (event: WheelEvent) => {
    event.preventDefault();
    this.zoomBy(event.deltaY < 0 ? 0.25 : -0.25);
  };

  private handleStagePointerDown = (event: FederatedPointerEvent) => {
    const point = this.world.toLocal(event.global);
    const path = findPath(
      this.layout,
      { x: this.local.x, y: this.local.y },
      point,
    );
    if (path.length === 0) return;
    this.pendingAgentId = null;
    this.local.path = path;
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
