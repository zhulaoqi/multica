import type {
  Application,
  Container,
  FederatedPointerEvent,
  Graphics,
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
  TILE,
  type OfficeLayout,
  type RoomId,
} from "./layout";
import { OfficeAgents, type OfficeAgentView, type OfficeHuman } from "./agents";
import { nearestAgent, nearestVisitSlot } from "./interactions";
import { isTypingTarget } from "./input";
import { buildRoom, type RoomView } from "./room";
import { loadOfficeTextures } from "./textures";
import { OfficeCat } from "./office-cat";
import { OfficeMinimap } from "./minimap";
import { OfficeWeather } from "./weather";
import type { SeasonTheme } from "../game/season";
import { seasonFor } from "../game/season";
import type { OfficeEventId } from "../game/events";
import {
  laterEventDelay,
  nextEventDelay,
  pickOfficeEvent,
} from "../game/events";
import type { DeskZone } from "../game/zones";

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
/**
 * Company sign on the reception back wall, centred over the front desk
 * (x 22..27) and kept clear of the doorway at x 27. Sorted just above the wall
 * so the receptionists standing in front of it still cover it.
 */
const COMPANY_SIGN = { centerX: 24.5 * 16, top: 25 * 16 - 7, zIndex: 26 * 16 + 1 } as const;
const COMPANY_SIGN_ACCENT = 0xf39800;
const MOVE_KEYS = ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "w", "a", "s", "d"];
/** Standing spot in front of the reception desk, where check-in happens. */
const RECEPTION_POINT = feetAtTile(24, 28);
/** Wide enough to check in without brushing the desk, narrow enough to read. */
const RECEPTION_RADIUS = 44;
/** Coffee machine and meeting-room board, both reachable by walking up. */
const COFFEE_POINT = feetAtTile(24, 4);
const COFFEE_RADIUS = 36;
const MEETING_POINT = feetAtTile(8, 10);
const MEETING_RADIUS = 36;
/** Rows of paper on the desk before the stack stops growing visually. */
const MAX_DESK_PAPERS = 9;
/** How long the archive stamp or a random event stays up. */
const ARCHIVE_RITUAL_MS = 2_600;
const EVENT_EFFECT_MS = 3_200;
const CONFETTI_MS = 3_800;
const CONFETTI_COUNT = 40;
const CONFETTI_COLORS = [0xf2c14e, 0x4f9bd9, 0x63b37a, 0xd97a7a, 0xb07ad9, 0xf8fafc];

/** Walk-up places other than agents, in interaction priority order. */
export type OfficePlace = "reception" | "coffee" | "meeting";

/** What a desk has collected from today's work. */
export interface DeskTrace {
  deskIndex: number;
  /** A mug after any completed run today. */
  mug?: boolean;
  /** Sheets waiting, drawn up to three. */
  papers?: number;
}

/** Department plaque drawn inside its room, under the room name. */
export interface RoomBadge {
  room: RoomId;
  label: string;
  level: number;
}

interface ConfettiPiece {
  view: Graphics;
  vy: number;
  sway: number;
  phase: number;
  spin: number;
}

export type { OfficeAgentView };

/** Long hierarchy paths would overrun the next cluster, so cap the plate. */
const MAX_PLAQUE_LABEL_CHARS = 18;

function fitPlaqueLabel(label: string): string {
  const chars = [...label];
  if (chars.length <= MAX_PLAQUE_LABEL_CHARS) return label;
  return `${chars.slice(0, MAX_PLAQUE_LABEL_CHARS - 1).join("")}…`;
}

/** Company levels are 1-based; anything unusable falls back to a new company. */
function normalizeCompanyLevel(level: number | undefined): number {
  if (!level || !Number.isFinite(level)) return 1;
  return Math.max(1, Math.floor(level));
}

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
  /** Shown on the sign behind the reception desk. */
  companyName: string;
  /** Company level; decorations unlock as it grows. Defaults to 1. */
  companyLevel?: number;
  /** Ambient theme; defaults to the current calendar season. */
  season?: SeasonTheme;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  /** The player reached an agent and wants to talk to it. */
  onAgentActivate: (agentId: string) => void;
  /** Nearest agent to the player changed, so the HUD can hint at interact. */
  onFocusAgentChange: (agentId: string | null) => void;
  /** The player walked up to the front desk, coffee machine, or board. */
  onPlaceFocusChange?: (place: OfficePlace | null) => void;
  /** A cosmetic office beat happened, so the HUD can show a one-line toast. */
  onRandomEvent?: (event: OfficeEventId) => void;
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
  /** Replaced when the company levels up and unlocks more furniture. */
  private layout: OfficeLayout;
  private readonly camera: Container;
  private readonly world: Container;
  private room: RoomView;
  private companyLevel: number;
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
  private readonly onPlaceFocusChange: (place: OfficePlace | null) => void;
  private readonly onRandomEvent: (event: OfficeEventId) => void;
  private focusedAgentId: string | null = null;
  private focusedPlace: OfficePlace | null = null;
  /** Agent the player is walking toward; activated once close enough. */
  private pendingAgentId: string | null = null;
  private papers: Container | null = null;
  private paperCount = 0;
  private confetti: Container | null = null;
  private confettiPieces: ConfettiPiece[] = [];
  private confettiUntil = 0;
  private traces: Container | null = null;
  private badges: Container | null = null;
  private zones: Container | null = null;
  private readonly weather: OfficeWeather;
  private readonly cat: OfficeCat;
  private readonly minimap: OfficeMinimap;
  private eventEffect: Container | null = null;
  private eventEffectUntil = 0;
  private nextEventAt = 0;
  private eventsEnabled = true;

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
    this.companyLevel = normalizeCompanyLevel(options.companyLevel);
    this.onLocalMove = options.onLocalMove;
    this.onAgentActivate = options.onAgentActivate;
    this.onFocusAgentChange = options.onFocusAgentChange;
    this.onPlaceFocusChange = options.onPlaceFocusChange ?? (() => {});
    this.onRandomEvent = options.onRandomEvent ?? (() => {});
    const season = options.season ?? seasonFor(new Date());

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

    this.weather = new OfficeWeather(
      pixi,
      app.stage,
      season,
      app.screen.width,
      app.screen.height,
    );
    this.cat = new OfficeCat(pixi, layout, world);
    this.minimap = new OfficeMinimap(pixi, app.stage, layout);
    this.minimap.resize(app.screen.width);
    this.nextEventAt = performance.now() + nextEventDelay();
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
    const layout = buildOfficeLayout(normalizeCompanyLevel(options.companyLevel));
    const room = buildRoom(pixi, layout);
    room.floor.zIndex = -1000;
    world.addChild(room.floor);
    for (const prop of room.props) world.addChild(prop);

    const scene = new OfficeScene(pixi, app, camera, world, room, options, layout);
    scene.addRoomLabels(options.roomNames);
    scene.addCompanySign(options.companyName);
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

  /**
   * Levels unlock office upgrades. Rebuilds the room in place: the player,
   * peers and agents stay where they are, but new furniture becomes solid and
   * the agents replan any walk in flight around it.
   */
  setCompanyLevel(companyLevel: number) {
    if (this.destroyed) return;
    const level = normalizeCompanyLevel(companyLevel);
    if (level === this.companyLevel) return;
    this.companyLevel = level;

    const layout = buildOfficeLayout(level);
    this.layout = layout;
    this.agents.setLayout(layout);

    const room = buildRoom(this.pixi, layout);
    room.floor.zIndex = -1000;
    this.world.removeChild(this.room.floor);
    for (const prop of this.room.props) this.world.removeChild(prop);
    this.room.floor.destroy({ children: true });
    for (const prop of this.room.props) prop.destroy({ children: true });
    this.world.addChild(room.floor);
    for (const prop of room.props) this.world.addChild(prop);
    this.room = room;
    this.agents.setScreens(room.screens);
    this.cat.setLayout(layout);
    this.minimap.setLayout(layout);
  }

  /** The wrapper resized; keep the same world area in view. */
  resize(width: number, height: number) {
    if (this.destroyed) return;
    this.app.renderer.resize(Math.max(1, width), Math.max(1, height));
    this.app.stage.hitArea = new this.pixi.Rectangle(0, 0, width, height);
    this.weather.resize(Math.max(1, width), Math.max(1, height));
    this.minimap.resize(Math.max(1, width));
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

  /** Walks the agent to the front desk to hand over a finished result. */
  deliverToReception(agentId: string) {
    if (this.destroyed) return;
    this.agents.deliverToReception(agentId);
  }

  /**
   * Papers on the front desk mirror work waiting for sign-off, so the queue
   * is visible from across the room without opening a panel.
   */
  setReceptionPapers(count: number) {
    if (this.destroyed) return;
    const next = Math.max(0, Math.floor(count));
    if (next === this.paperCount) return;
    this.paperCount = next;
    this.drawReceptionPapers();
  }

  /** Swaps the ambient weather; the season chip changes at the same time. */
  setSeason(theme: SeasonTheme) {
    if (this.destroyed) return;
    this.weather.setTheme(theme);
  }

  setMinimapVisible(visible: boolean) {
    if (this.destroyed) return;
    this.minimap.setVisible(visible);
  }

  isMinimapVisible(): boolean {
    return this.minimap.isVisible();
  }

  setEventsEnabled(enabled: boolean) {
    this.eventsEnabled = enabled;
  }

  /** Mugs and papers left on a desk, mirroring today's work. */
  setDeskTraces(traces: readonly DeskTrace[]) {
    if (this.destroyed) return;
    this.traces?.destroy({ children: true });
    this.traces = null;
    if (traces.length === 0) return;
    const layer = new this.pixi.Container();
    for (const trace of traces) {
      const slot = this.layout.agents[trace.deskIndex];
      if (!slot) continue;
      const left = slot.desk.x * 16;
      const top = slot.desk.y * 16;
      const z = (slot.desk.y + 1) * 16 + 2;
      if (trace.mug) {
        const mug = new this.pixi.Graphics();
        mug.rect(left + 7, top + 2, 5, 5).fill(0xf8fafc);
        mug.rect(left + 8, top + 3, 3, 2).fill(0x8b5a3c);
        mug.rect(left + 12, top + 3, 2, 2).fill(0xf8fafc);
        mug.zIndex = z;
        layer.addChild(mug);
      }
      const papers = Math.min(3, Math.max(0, Math.floor(trace.papers ?? 0)));
      for (let index = 0; index < papers; index++) {
        const sheet = new this.pixi.Graphics();
        sheet
          .rect(left + 18 + index, top + 2 + index, 8, 5)
          .fill(0xf8fafc)
          .rect(left + 18 + index, top + 6 + index, 8, 1)
          .fill(0xc7ced6);
        sheet.zIndex = z;
        layer.addChild(sheet);
      }
    }
    layer.sortableChildren = true;
    this.world.addChild(layer);
    this.traces = layer;
  }

  /** A name-and-level plate, used for room and desk-zone plaques. */
  private createPlaque(
    label: string,
    level: number,
    standing = false,
  ): Container {
    const plate = new this.pixi.Container();
    const text = new this.pixi.Text({
      text: `${fitPlaqueLabel(label)} · Lv ${level}`,
      style: {
        fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
        fontSize: 7,
        fontWeight: "600",
        fill: 0xe8f4fb,
      },
    });
    text.resolution = 4;
    text.position.set(4, 1);
    const width = Math.ceil(text.width) + 8;
    const height = Math.ceil(text.height) + 3;
    const background = new this.pixi.Graphics();
    background
      .roundRect(0, 0, width, height, 2)
      .fill({ color: 0x5aa8a2, alpha: 0.82 });
    if (standing) {
      // Desk-zone signs are physical: a short post stands them on the aisle
      // instead of leaving a label floating over the agents' heads.
      const postX = Math.round(width / 2);
      const stand = new this.pixi.Graphics();
      stand.rect(postX - 1, height, 2, 18).fill(0x4b938e);
      stand.rect(postX - 7, height + 16, 14, 2).fill(0x3c7a76);
      plate.addChild(stand);
    }
    plate.addChild(background, text);
    plate.eventMode = "none";
    return plate;
  }

  /** Department name and level on each room's wall. */
  setRoomBadges(badges: readonly RoomBadge[]) {
    if (this.destroyed) return;
    this.badges?.destroy({ children: true });
    this.badges = null;
    if (badges.length === 0) return;
    const layer = new this.pixi.Container();
    for (const badge of badges) {
      const room = this.layout.rooms.find((entry) => entry.id === badge.room);
      if (!room) continue;
      const plate = this.createPlaque(badge.label, badge.level);
      plate.position.set(room.rect.x * TILE + 3, room.rect.y * TILE + 16);
      layer.addChild(plate);
    }
    layer.sortableChildren = true;
    layer.zIndex = ROOM_LABEL_Z + 1;
    this.world.addChild(layer);
    this.badges = layer;
  }

  /** Department name and level over each desk cluster. */
  setDeskZones(zones: readonly DeskZone[]) {
    if (this.destroyed) return;
    this.zones?.destroy({ children: true });
    this.zones = null;
    if (zones.length === 0) return;
    const layer = new this.pixi.Container();
    for (const zone of zones) {
      const plate = this.createPlaque(zone.pathLabel, zone.level, true);
      // Lift the board off the floor so its feet land on the aisle tile.
      plate.position.set(zone.anchor.tx * TILE, zone.anchor.ty * TILE - 16);
      layer.addChild(plate);
    }
    layer.sortableChildren = true;
    // A standing sign belongs to the floor plane: agents north of it pass
    // behind, and anyone walking south of it passes in front.
    layer.zIndex = Math.max(...zones.map((zone) => (zone.anchor.ty + 1) * TILE));
    this.world.addChild(layer);
    this.zones = layer;
  }

  /**
   * Closing the loop on delivered work: everyone checks, confetti falls, and
   * a stamped folder sits on the front desk for a moment.
   */
  archiveRitual() {
    if (this.destroyed) return;
    this.celebrate();
    const now = performance.now();
    this.eventEffect?.destroy({ children: true });
    const layer = new this.pixi.Container();
    const folder = new this.pixi.Graphics();
    const left = 24 * 16 - 2;
    const top = 27 * 16 + 2;
    folder.rect(left, top, 11, 7).fill(0xd9a34f);
    folder.rect(left + 1, top + 1, 9, 2).fill(0xf1c27a);
    folder.rect(left + 5, top + 3, 3, 1).fill(0x2f4858);
    folder.rect(left + 4, top + 4, 5, 1).fill(0x2f4858);
    folder.zIndex = 28 * 16 + 2;
    layer.addChild(folder);
    this.world.addChild(layer);
    this.eventEffect = layer;
    this.eventEffectUntil = now + ARCHIVE_RITUAL_MS;
  }

  /** Good news: confetti over the view and a check from every agent. */
  celebrate() {
    if (this.destroyed) return;
    const now = performance.now();
    this.agents.celebrate(now);

    this.confetti?.destroy({ children: true });
    const layer = new this.pixi.Container();
    this.app.stage.addChild(layer);
    const width = this.app.screen.width;
    const pieces: ConfettiPiece[] = [];
    for (let index = 0; index < CONFETTI_COUNT; index++) {
      const view = new this.pixi.Graphics();
      const color = CONFETTI_COLORS[index % CONFETTI_COLORS.length]!;
      view.rect(-2, -1.5, 4, 3).fill(color);
      view.position.set(Math.random() * width, -Math.random() * 80 - 6);
      layer.addChild(view);
      pieces.push({
        view,
        vy: 70 + Math.random() * 110,
        sway: 12 + Math.random() * 24,
        phase: Math.random() * Math.PI * 2,
        spin: -3 + Math.random() * 6,
      });
    }
    this.confetti = layer;
    this.confettiPieces = pieces;
    this.confettiUntil = now + CONFETTI_MS;
  }

  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    this.detachInput();
    this.app.ticker.remove(this.update);
    this.agents.destroy();
    this.cat.destroy();
    this.weather.destroy();
    this.minimap.destroy();
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
    if (id !== this.focusedAgentId) {
      this.focusedAgentId = id;
      this.onFocusAgentChange(id);
    }

    // Talking to a person outranks every desk and board behind them: the
    // agent's hint wins whenever one is in range.
    const place = id === null ? this.placeAt() : null;
    if (place !== this.focusedPlace) {
      this.focusedPlace = place;
      this.onPlaceFocusChange(place);
    }
  }

  /** Closest walk-up place, or null when the player is not at one. */
  private placeAt(): OfficePlace | null {
    const within = (point: { x: number; y: number }, radius: number) =>
      Math.hypot(point.x - this.local.x, point.y - this.local.y) <= radius;
    if (within(RECEPTION_POINT, RECEPTION_RADIUS)) return "reception";
    if (within(COFFEE_POINT, COFFEE_RADIUS)) return "coffee";
    if (within(MEETING_POINT, MEETING_RADIUS)) return "meeting";
    return null;
  }

  /** Draws the paper stack on the desk; the count is a read-only queue view. */
  private drawReceptionPapers() {
    this.papers?.destroy({ children: true });
    this.papers = null;
    if (this.paperCount <= 0) return;
    const stack = new this.pixi.Container();
    const shown = Math.min(this.paperCount, MAX_DESK_PAPERS);
    for (let index = 0; index < shown; index++) {
      const sheet = new this.pixi.Graphics();
      const x = 22 * 16 + 6 + (index % 3) * 9;
      const y = 27 * 16 + 3 + Math.floor(index / 3) * 5;
      sheet.rect(x, y, 8, 5).fill(0xf8fafc);
      sheet.rect(x, y + 4, 8, 1).fill(0xc7ced6);
      stack.addChild(sheet);
    }
    stack.zIndex = 28 * 16 + 1;
    this.world.addChild(stack);
    this.papers = stack;
  }

  /** Screen-space confetti; it outlives one camera pan and then cleans up. */
  private stepConfetti(now: number, dt: number) {
    if (!this.confetti) return;
    if (now >= this.confettiUntil) {
      this.confetti.destroy({ children: true });
      this.confetti = null;
      this.confettiPieces = [];
      return;
    }
    const height = this.app.screen.height;
    const seconds = now / 1000;
    for (const piece of this.confettiPieces) {
      piece.view.y += piece.vy * dt;
      piece.view.x += Math.sin(seconds * 2 + piece.phase) * piece.sway * dt;
      piece.view.rotation += piece.spin * dt;
      if (piece.view.y > height + 8) piece.view.y = -8;
    }
  }

  /**
   * Random office beats. They are pure presentation: a box is not a task, the
   * phone is not a notification, and turning them off loses no state.
   */
  private stepRandomEvents(now: number) {
    if (!this.eventsEnabled || now < this.nextEventAt) return;
    this.nextEventAt = now + laterEventDelay();
    const event = pickOfficeEvent();
    this.renderEvent(event);
    this.onRandomEvent(event);
  }

  private renderEvent(event: OfficeEventId) {
    this.eventEffect?.destroy({ children: true });
    this.eventEffect = null;
    const layer = new this.pixi.Container();
    const at = (tx: number, ty: number) => feetAtTile(tx, ty);
    switch (event) {
      case "cat": {
        this.cat.meow(performance.now());
        this.cat.scamper(performance.now());
        return;
      }
      case "delivery": {
        const spot = at(27, 32);
        const box = new this.pixi.Graphics();
        box.rect(spot.x - 7, spot.y - 12, 14, 11).fill(0xb98a54);
        box.rect(spot.x - 7, spot.y - 12, 14, 2).fill(0xd9a970);
        box.rect(spot.x - 1, spot.y - 12, 2, 11).fill(0x8a643a);
        box.zIndex = spot.y;
        layer.addChild(box);
        break;
      }
      case "coffee": {
        for (let index = 0; index < 3; index++) {
          const puff = new this.pixi.Graphics();
          puff
            .circle(23 * 16 + 10, 3 * 16 - 14 - index * 6, 3 + index)
            .fill({ color: 0xf8fafc, alpha: 0.4 - index * 0.1 });
          puff.zIndex = 3 * 16 + 2;
          layer.addChild(puff);
        }
        break;
      }
      case "plane": {
        const spot = at(31, 30);
        const plane = new this.pixi.Graphics();
        plane.rect(spot.x, spot.y - 18, 9, 3).fill(0xf8fafc);
        plane.rect(spot.x + 6, spot.y - 21, 5, 3).fill(0xe2e8f0);
        plane.rect(spot.x + 2, spot.y - 15, 2, 2).fill({ color: 0xcbd5e1, alpha: 0.6 });
        plane.zIndex = spot.y;
        layer.addChild(plane);
        break;
      }
      case "phone": {
        const spot = at(23, 26);
        const ring = new this.pixi.Graphics();
        ring.circle(spot.x + 6, spot.y - 26, 4).stroke({ color: 0xf2c14e, width: 1.5 });
        ring.circle(spot.x + 6, spot.y - 26, 7).stroke({ color: 0xf2c14e, width: 1, alpha: 0.5 });
        ring.zIndex = 28 * 16;
        layer.addChild(ring);
        break;
      }
      case "lights": {
        for (const [tx, ty] of [[8, 16], [24, 16], [44, 16]] as const) {
          const spot = at(tx, ty);
          const glow = new this.pixi.Graphics();
          glow
            .ellipse(spot.x, spot.y, 34, 16)
            .fill({ color: 0xf5d98a, alpha: 0.16 });
          glow.zIndex = ty * 16;
          layer.addChild(glow);
        }
        break;
      }
    }
    this.world.addChild(layer);
    this.eventEffect = layer;
    this.eventEffectUntil = performance.now() + EVENT_EFFECT_MS;
  }

  private stepEventEffect(now: number) {
    if (!this.eventEffect || now < this.eventEffectUntil) return;
    this.eventEffect.destroy({ children: true });
    this.eventEffect = null;
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

  private addCompanySign(name: string) {
    if (!name.trim()) return;
    const sign = new this.pixi.Container();
    const text = new this.pixi.Text({
      text: name,
      style: {
        fontFamily: "ui-sans-serif, system-ui, -apple-system, sans-serif",
        fontSize: 7,
        fontWeight: "700",
        fill: 0xf8fafc,
        letterSpacing: 0.3,
      },
    });
    text.resolution = 6;
    const width = Math.ceil(text.width) + 10;
    const height = Math.ceil(text.height) + 4;
    text.position.set(7, 2);
    const plate = new this.pixi.Graphics();
    plate.rect(0, 0, width, height).fill(0x1f2430);
    plate.rect(1, 1, width - 2, height - 2).fill(0x12304f);
    plate.rect(2, 2, 2, height - 4).fill(COMPANY_SIGN_ACCENT);
    sign.addChild(plate, text);
    sign.position.set(Math.round(COMPANY_SIGN.centerX - width / 2), COMPANY_SIGN.top);
    sign.zIndex = COMPANY_SIGN.zIndex;
    sign.eventMode = "none";
    this.world.addChild(sign);
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
    this.cat.update(dt, now);
    this.weather.update(dt);
    this.stepConfetti(now, dt);
    this.stepRandomEvents(now);
    this.stepEventEffect(now);
    this.minimap.update(
      { x: this.local.x, y: this.local.y },
      this.agents.statuses(),
      Array.from(this.peers.values(), (peer) => ({ x: peer.x, y: peer.y })),
      this.cat.position(),
    );
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
