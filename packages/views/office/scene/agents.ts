/**
 * Draws and animates one character per agent: working agents walk to their
 * desk and type, queued agents gather at the meeting table (the visible
 * collaboration pose), idle agents drift between their desk and shared spots,
 * and offline agents sit dimmed with a sleep balloon. Anyone standing next to
 * an agent gets a chat balloon from it.
 *
 * Everything here is local presentation: React only hands over id, name, and
 * status, and the module never fetches.
 */

import type { Container, Graphics, Text as PixiText } from "pixi.js";
import {
  createCharacter,
  type AgentDeskStatus,
  type BubbleKind,
  type CharacterPose,
  type CharacterView,
} from "./characters";
import {
  directionFor,
  findPath,
  stepTowards,
  TILE,
  type OfficeLayout,
} from "./layout";
import {
  assignStations,
  deskStation,
  idleErrandAt,
  leisureStations,
  meetingStations,
  type Station,
} from "./stations";
import type { InteractableAgent } from "./interactions";
import { hashSeed } from "./room";

const WALK_SPEED = 74;
const CHAT_RADIUS = 26;
const AGENT_CHAT_RADIUS = 18;
const CHAT_MS = 1200;
const CHAT_COOLDOWN_MS = 3500;
const CHAT_COOLDOWN_SPREAD_MS = 4500;
const ERRAND_MIN_MS = 5000;
const ERRAND_SPREAD_MS = 6000;
const SPAWN_STAGGER_MS = 280;
const EMOTE_MS = 1600;
const GLOW_COLOR = 0x9fe3f5;

export interface OfficeAgentView {
  id: string;
  name: string;
  status: AgentDeskStatus;
}

/** A human avatar the agents can react to (the local player and peers). */
export interface OfficeHuman {
  x: number;
  y: number;
}

interface Avatar {
  id: string;
  view: CharacterView;
  label: PixiText;
  status: AgentDeskStatus;
  home: Station;
  x: number;
  y: number;
  path: { x: number; y: number }[];
  goalKey: string;
  errandStation: Station | null;
  errandStep: number;
  errandUntil: number;
  meetingSeat: Station | null;
  leisureSpot: Station | null;
  chatUntil: number;
  nextChatAt: number;
  /** One-off reaction (ack, success) that outranks the routine balloon. */
  emoteKind: BubbleKind | null;
  emoteUntil: number;
  moving: boolean;
  glow: Graphics | null;
  /** Wall clock at which the avatar steps through the doorway; null while queued. */
  spawnAt: number | null;
}

export interface OfficeAgentsOptions {
  pixi: typeof import("pixi.js");
  layout: OfficeLayout;
  world: Container;
  createLabel: (text: string) => PixiText;
  onAgentActivate: (agentId: string) => void;
}

export class OfficeAgents {
  private readonly pixi: typeof import("pixi.js");
  private readonly layout: OfficeLayout;
  private readonly world: Container;
  private readonly createLabel: (text: string) => PixiText;
  private readonly onAgentActivate: (agentId: string) => void;
  private readonly homes: Station[];
  private readonly meeting: Station[];
  private readonly leisure: Station[];
  private readonly glowByIndex = new Map<number, Graphics>();
  private readonly avatars = new Map<string, Avatar>();
  private spawnQueue = 0;
  private engagedId: string | null = null;

  constructor(options: OfficeAgentsOptions) {
    this.pixi = options.pixi;
    this.layout = options.layout;
    this.world = options.world;
    this.createLabel = options.createLabel;
    this.onAgentActivate = options.onAgentActivate;

    this.meeting = meetingStations(options.layout);
    this.leisure = leisureStations(options.layout);
    this.homes = [
      ...options.layout.agents.map((slot, index) => deskStation(slot, index)),
      ...options.layout.overflowSpots.map((spot, index) => ({
        key: `overflow:${index}`,
        x: spot.x,
        y: spot.y,
        dir: spot.dir,
        kind: "desk" as const,
        seat: false,
      })),
    ];

    options.layout.agents.forEach((slot, index) => {
      const glow = new this.pixi.Graphics();
      glow
        .rect(slot.desk.x * TILE + 7, slot.desk.y * TILE - 13, 18, 13)
        .fill({ color: GLOW_COLOR, alpha: 0.22 });
      glow.alpha = 0;
      this.world.addChild(glow);
      this.glowByIndex.set(index, glow);
    });
  }

  setAgents(agents: OfficeAgentView[]) {
    const present = new Set<string>();
    agents.forEach((agent, index) => {
      const home = this.homes[index];
      if (!home) return;
      present.add(agent.id);
      const existing = this.avatars.get(agent.id);
      if (existing) {
        existing.status = agent.status;
        existing.view.setStatus(agent.status);
        if (existing.label.text !== agent.name) existing.label.text = agent.name;
        return;
      }
      const view = createCharacter(this.pixi, {
        kind: "agent",
        seed: agent.id,
        status: agent.status,
      });
      // Agents walk in from the doorway instead of popping into their chairs.
      const entrance = this.entranceFor(index);
      view.container.position.set(entrance.x, entrance.y);
      view.container.visible = false;
      view.setDir("up");
      view.container.eventMode = "static";
      view.container.cursor = "pointer";
      view.container.hitArea = new this.pixi.Rectangle(-8, -30, 16, 32);
      view.container.on("pointerdown", (event) => {
        event.stopPropagation();
        this.onAgentActivate(agent.id);
      });
      this.world.addChild(view.container);
      this.avatars.set(agent.id, {
        id: agent.id,
        view,
        label: this.createLabel(agent.name),
        status: agent.status,
        home,
        x: entrance.x,
        y: entrance.y,
        path: [],
        goalKey: "",
        errandStation: null,
        errandStep: 0,
        errandUntil: 0,
        meetingSeat: null,
        leisureSpot: null,
        chatUntil: 0,
        nextChatAt: 0,
        emoteKind: null,
        emoteUntil: 0,
        moving: false,
        glow: this.glowByIndex.get(index) ?? null,
        spawnAt: null,
      });
    });

    for (const [id, avatar] of this.avatars) {
      if (present.has(id)) continue;
      this.dispose(avatar);
      this.avatars.delete(id);
    }
  }

  /**
   * Marks the agent the panel is open for. Engagement stops idle wandering and
   * turns the agent toward the nearest human; null returns it to its routine.
   * The id is kept even if the avatar is not on stage yet.
   */
  engage(agentId: string | null) {
    this.engagedId = agentId;
  }

  /** Visible agents, in world space, for proximity checks in the scene. */
  positions(): InteractableAgent[] {
    const positions: InteractableAgent[] = [];
    for (const avatar of this.avatars.values()) {
      if (!avatar.view.container.visible) continue;
      positions.push({ id: avatar.id, x: avatar.x, y: avatar.y });
    }
    return positions;
  }

  positionOf(agentId: string): InteractableAgent | null {
    const avatar = this.avatars.get(agentId);
    if (!avatar || !avatar.view.container.visible) return null;
    return { id: avatar.id, x: avatar.x, y: avatar.y };
  }

  /** Shows a one-off balloon, e.g. a check after a task is assigned. */
  playEmote(agentId: string, kind: BubbleKind, nowMs: number, durationMs = EMOTE_MS) {
    const avatar = this.avatars.get(agentId);
    if (!avatar) return;
    avatar.emoteKind = kind;
    avatar.emoteUntil = nowMs + durationMs;
  }

  update(dtSeconds: number, nowMs: number, humans: OfficeHuman[]) {
    // Newcomers file through the doorway one at a time instead of walking in
    // as a clump; a whole batch arriving later still gets spaced out.
    let queuedSpawns = 0;
    for (const avatar of this.avatars.values()) {
      if (avatar.spawnAt !== null) continue;
      avatar.spawnAt = nowMs + this.spawnQueue * SPAWN_STAGGER_MS;
      this.spawnQueue += 1;
      queuedSpawns += 1;
    }
    if (queuedSpawns === 0) this.spawnQueue = 0;

    this.assignSharedSpots();
    for (const avatar of this.avatars.values()) {
      if (avatar.spawnAt !== null && nowMs < avatar.spawnAt) continue;
      if (!avatar.view.container.visible) avatar.view.container.visible = true;

      let target: Station;
      if (this.holdsPosition(avatar)) {
        // Talking to the local player: stay put and drop the routine so
        // leaving the conversation replans from wherever it stopped.
        target = this.holdStation(avatar, humans);
        avatar.goalKey = "";
        avatar.path = [];
      } else {
        target = this.stationFor(avatar, nowMs);
        if (target.key !== avatar.goalKey) {
          avatar.goalKey = target.key;
          avatar.path = findPath(
            this.layout,
            { x: avatar.x, y: avatar.y },
            { x: target.x, y: target.y },
          );
        }
      }

      const moving = this.advance(avatar, dtSeconds, target);
      avatar.moving = moving;
      avatar.view.setWalking(moving);
      if (moving) {
        avatar.view.setPose("stand");
      } else {
        avatar.view.setPose(this.poseFor(avatar, target));
        avatar.view.setDir(target.dir);
      }

      const human = nearestHuman(humans, avatar.x, avatar.y);
      if (human && !moving) {
        if (!target.seat) {
          avatar.view.setDir(
            directionFor(human.x - avatar.x, human.y - avatar.y) ?? target.dir,
          );
        }
        // Greet in bursts rather than chattering non-stop while someone stands nearby.
        if (nowMs >= avatar.nextChatAt) {
          avatar.chatUntil = nowMs + CHAT_MS;
          avatar.nextChatAt = nowMs + CHAT_MS + this.chatCooldown(avatar.id);
        }
      }

      avatar.view.tick(nowMs);
      this.pulseGlow(avatar, target, nowMs);
    }
    this.stepAgentChat(nowMs);
    for (const avatar of this.avatars.values()) {
      avatar.view.setBubble(this.bubbleFor(avatar, nowMs));
    }
  }

  /** Two agents standing together chat, which is the "talking shop" beat. */
  private stepAgentChat(nowMs: number) {
    const list = [...this.avatars.values()].filter(
      (avatar) => !avatar.moving && avatar.view.container.visible,
    );
    for (let i = 0; i < list.length; i++) {
      for (let j = i + 1; j < list.length; j++) {
        const a = list[i]!;
        const b = list[j]!;
        if (nowMs < a.nextChatAt || nowMs < b.nextChatAt) continue;
        if (Math.hypot(a.x - b.x, a.y - b.y) >= AGENT_CHAT_RADIUS) continue;
        const cooldown = this.chatCooldown(`${a.id}:${b.id}`);
        a.chatUntil = nowMs + CHAT_MS;
        b.chatUntil = nowMs + CHAT_MS;
        a.nextChatAt = nowMs + CHAT_MS + cooldown;
        b.nextChatAt = nowMs + CHAT_MS + cooldown;
      }
    }
  }

  private chatCooldown(seed: string): number {
    return CHAT_COOLDOWN_MS + (hashSeed(seed) % CHAT_COOLDOWN_SPREAD_MS);
  }

  syncLabels(zoom: number) {
    for (const avatar of this.avatars.values()) {
      avatar.label.visible = avatar.view.container.visible;
      avatar.label.position.set(
        Math.round(avatar.view.container.x * zoom),
        Math.round(avatar.view.container.y * zoom) + 16,
      );
    }
  }

  destroy() {
    for (const avatar of this.avatars.values()) this.dispose(avatar);
    this.avatars.clear();
    for (const glow of this.glowByIndex.values()) glow.destroy();
    this.glowByIndex.clear();
  }

  /** Queued agents take turns at the table; idle agents take turns at spots. */
  private assignSharedSpots() {
    const queued = [...this.avatars.values()]
      .filter((avatar) => avatar.status === "queued")
      .sort((a, b) => a.id.localeCompare(b.id));
    queued.forEach((avatar, index) => {
      avatar.meetingSeat = assignStations(this.meeting, index);
    });

    const idle = [...this.avatars.values()]
      .filter((avatar) => avatar.status === "idle")
      .sort((a, b) => a.id.localeCompare(b.id));
    idle.forEach((avatar, index) => {
      avatar.leisureSpot = assignStations(this.leisure, index);
    });
  }

  /** Walk-in points inside the bottom doorway, spread so avatars never stack. */
  private entranceFor(index: number): { x: number; y: number } {
    return {
      x: (13.5 + (index % 4)) * TILE,
      y: 16 * TILE + 14 - Math.floor(index / 4) * 9,
    };
  }

  /** Engaged agents pause their routine once they finish the current step. */
  private holdsPosition(avatar: Avatar): boolean {
    return (
      avatar.id === this.engagedId &&
      !avatar.moving &&
      avatar.path.length === 0
    );
  }

  private holdStation(avatar: Avatar, humans: OfficeHuman[]): Station {
    const human = nearestHuman(humans, avatar.x, avatar.y);
    const canTurn = avatar.status !== "working" && avatar.status !== "offline";
    return {
      key: "engage",
      x: avatar.x,
      y: avatar.y,
      dir:
        canTurn && human
          ? directionFor(human.x - avatar.x, human.y - avatar.y) ?? avatar.home.dir
          : avatar.home.dir,
      kind: avatar.home.kind,
      // Seated routines keep their pose; everyone else turns to face the player.
      seat: !canTurn,
    };
  }

  private stationFor(avatar: Avatar, nowMs: number): Station {
    switch (avatar.status) {
      case "working":
      case "offline":
        return avatar.home;
      case "queued":
        return avatar.meetingSeat ?? avatar.home;
      case "idle": {
        // Stagger the first errand so the whole room does not stand up at once.
        if (avatar.errandUntil === 0) {
          avatar.errandUntil = nowMs + (hashSeed(avatar.id) % ERRAND_SPREAD_MS);
          return avatar.home;
        }
        if (nowMs < avatar.errandUntil && avatar.errandStation) {
          return avatar.errandStation;
        }
        avatar.errandStep += 1;
        const next = idleErrandAt(avatar.errandStep, avatar.home, avatar.leisureSpot);
        avatar.errandStation = next;
        avatar.errandUntil =
          nowMs + ERRAND_MIN_MS + (hashSeed(`${avatar.id}:${avatar.errandStep}`) % ERRAND_SPREAD_MS);
        return next;
      }
    }
  }

  private poseFor(avatar: Avatar, target: Station): CharacterPose {
    if (avatar.status === "offline") return "sleep";
    if (avatar.status === "working") return "type";
    if (avatar.id === this.engagedId) return "stand";
    if (avatar.status === "queued") return "read";
    return target.seat ? "read" : "stand";
  }

  private bubbleFor(avatar: Avatar, nowMs: number): BubbleKind | null {
    if (nowMs < avatar.emoteUntil && avatar.emoteKind) return avatar.emoteKind;
    if (avatar.status === "offline") return "sleep";
    if (avatar.id === this.engagedId) return "chat";
    if (nowMs < avatar.chatUntil) return "chat";
    if (avatar.status === "queued") return "dots";
    return null;
  }

  /** Steps toward the station; true while still moving. */
  private advance(avatar: Avatar, dtSeconds: number, target: Station): boolean {
    let remaining = WALK_SPEED * dtSeconds;
    const view = avatar.view.container;
    let moving = false;

    while (remaining > 0 && avatar.path.length > 0) {
      const waypoint = avatar.path[0]!;
      const distance = Math.hypot(waypoint.x - avatar.x, waypoint.y - avatar.y);
      const step = stepTowards({ x: avatar.x, y: avatar.y }, waypoint, remaining);
      avatar.x = step.x;
      avatar.y = step.y;
      if (step.dir) avatar.view.setDir(step.dir);
      moving = true;
      if (step.arrived) {
        avatar.path.shift();
        remaining -= distance;
      } else {
        remaining = 0;
      }
    }

    if (remaining > 0) {
      const distance = Math.hypot(target.x - avatar.x, target.y - avatar.y);
      if (distance > 0.6) {
        const step = stepTowards({ x: avatar.x, y: avatar.y }, target, remaining);
        avatar.x = step.x;
        avatar.y = step.y;
        if (step.dir) avatar.view.setDir(step.dir);
        moving = true;
      }
    }

    view.position.set(Math.round(avatar.x), Math.round(avatar.y));
    return moving;
  }

  private pulseGlow(avatar: Avatar, target: Station, nowMs: number) {
    if (!avatar.glow) return;
    const atHome =
      target.kind === "desk" &&
      Math.hypot(avatar.x - avatar.home.x, avatar.y - avatar.home.y) < 2;
    const phase = 0.55 + 0.45 * Math.sin(nowMs / 320);
    avatar.glow.alpha =
      avatar.status === "working" && atHome ? 0.22 + 0.2 * phase : 0;
  }

  private dispose(avatar: Avatar) {
    avatar.view.container.destroy({ children: true });
    avatar.label.destroy();
  }
}

function nearestHuman(humans: OfficeHuman[], x: number, y: number): OfficeHuman | null {
  let best: OfficeHuman | null = null;
  let bestDistance = CHAT_RADIUS;
  for (const human of humans) {
    const distance = Math.hypot(human.x - x, human.y - y);
    if (distance < bestDistance) {
      bestDistance = distance;
      best = human;
    }
  }
  return best;
}
