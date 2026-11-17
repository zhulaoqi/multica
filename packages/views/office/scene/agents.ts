/**
 * One character per agent, driven by the behaviour runtime.
 *
 * Presence decides the root routine: working agents type in bursts and run
 * errands, queued agents sit at the meeting table, idle agents wander between
 * lounge spots, and offline agents walk out and vanish. Errands are pushed on
 * top of the current routine, so an agent that leaves for coffee comes back to
 * exactly the typing burst it interrupted.
 *
 * Everything here is local presentation: React only hands over id, name, and
 * status, and the module never fetches.
 */

import type { Container, Text as PixiText } from "pixi.js";
import {
  createCharacter,
  type AgentDeskStatus,
  type BubbleKind,
  type CharacterView,
} from "./characters";
import {
  directionFor,
  findPath,
  stepTowards,
  TILE,
  type OfficeLayout,
  type Station,
} from "./layout";
import { nearestVisitSlot, type InteractableAgent } from "./interactions";
import { pickVisitPair } from "./social";
import { Behavior, type Action } from "./behavior/behavior";
import { createRng } from "./behavior/rng";
import { Reservations } from "./behavior/reservations";
import {
  idleRoot,
  leaveRoutine,
  meetingRoutine,
  workRoot,
  type RoutineEnv,
} from "./behavior/routines";
import { hashSeed, type DeskScreen } from "./room";

const WALK_SPEED = 84;
const CHAT_RADIUS = 26;
const AGENT_CHAT_RADIUS = 20;
const CHAT_MS = 1200;
const CHAT_COOLDOWN_MS = 3500;
const CHAT_COOLDOWN_SPREAD_MS = 4500;
const SPAWN_STAGGER_MS = 320;
const EMOTE_MS = 1800;
/** How far apart two agents may be to consider a walk-over chat. */
const SOCIAL_RADIUS = 260;
const SOCIAL_COOLDOWN_MS = 13_000;
const SOCIAL_COOLDOWN_SPREAD_MS = 12_000;
const FIRST_SOCIAL_DELAY_MS = 9_000;
const TALK_MS_MIN = 3_800;
const TALK_MS_SPREAD = 2_600;
const BUBBLE_MS_MIN = 1_300;
const BUBBLE_MS_SPREAD = 900;

export interface OfficeAgentView {
  id: string;
  name: string;
  status: AgentDeskStatus;
  /** What the agent is doing right now, shown in a bubble over its head. */
  activity?: string | null;
}

/** A human avatar the agents can react to (the local player and peers). */
export interface OfficeHuman {
  x: number;
  y: number;
}

/**
 * An agent walking over to a colleague's desk for a chat. The visitor's
 * routine is suspended, not restarted, so it picks up exactly where it left
 * off after the conversation.
 */
interface VisitState {
  targetId: string;
  phase: "walk" | "talk" | "return";
  until: number;
  nextBubbleAt: number;
}

interface Avatar {
  id: string;
  view: CharacterView;
  label: PixiText;
  status: AgentDeskStatus;
  home: Station;
  env: RoutineEnv;
  brain: Behavior;
  x: number;
  y: number;
  path: { x: number; y: number }[];
  goalKey: string;
  moving: boolean;
  chatUntil: number;
  nextChatAt: number;
  /** One-off reaction (ack, success) that outranks the routine balloon. */
  emoteKind: BubbleKind | null;
  emoteUntil: number;
  /** False until the staggered walk-in starts. */
  active: boolean;
  /** Wall clock at which the walk-in begins; null while unassigned. */
  spawnAt: number | null;
  hidden: boolean;
  screen: DeskScreen | null;
  /** Colleague this agent is walking over to, when it is the visitor. */
  visit: VisitState | null;
  /** Colleague standing right here talking to it, when it is the host. */
  chatPartnerId: string | null;
  /** Earliest wall clock at which this agent will start a social visit. */
  nextVisitAt: number;
  /** Next chat balloon beat during a conversation. */
  nextBubbleAt: number;
}

export interface OfficeAgentsOptions {
  pixi: typeof import("pixi.js");
  layout: OfficeLayout;
  world: Container;
  screens: DeskScreen[];
  createLabel: (text: string) => PixiText;
  onAgentActivate: (agentId: string) => void;
}

export class OfficeAgents {
  private readonly pixi: typeof import("pixi.js");
  private readonly layout: OfficeLayout;
  private readonly world: Container;
  private readonly createLabel: (text: string) => PixiText;
  private readonly onAgentActivate: (agentId: string) => void;
  private readonly stations: Station[];
  private readonly homes: Station[];
  private readonly meetingSeats: Station[];
  private readonly reservations: Reservations;
  private readonly screens: DeskScreen[];
  private readonly avatars = new Map<string, Avatar>();
  private spawnQueue = 0;
  private engagedId: string | null = null;
  /** Wall clock before which no new social visit starts. */
  private nextSocialAt = 0;

  constructor(options: OfficeAgentsOptions) {
    this.pixi = options.pixi;
    this.layout = options.layout;
    this.world = options.world;
    this.createLabel = options.createLabel;
    this.onAgentActivate = options.onAgentActivate;
    this.screens = options.screens;

    const overflow: Station[] = options.layout.overflowSpots.map((spot, index) => ({
      key: `overflow:${index}`,
      x: spot.x,
      y: spot.y,
      dir: spot.dir,
      kind: "desk",
      seat: false,
      capacity: 1,
    }));

    this.stations = [...options.layout.stations, ...overflow];
    this.meetingSeats = this.stations.filter((station) => station.kind === "meeting");
    this.homes = [
      ...options.layout.agents.map((_, index) => this.stations.find((s) => s.key === `desk:${index}`)!),
      ...overflow,
    ];
    this.reservations = new Reservations(this.stations);
  }

  setAgents(agents: OfficeAgentView[]) {
    const present = new Set<string>();
    agents.forEach((agent, index) => {
      const home = this.homes[index];
      if (!home) return;
      present.add(agent.id);
      const existing = this.avatars.get(agent.id);
      if (existing) {
        if (existing.label.text !== agent.name) existing.label.text = agent.name;
        existing.view.setCaption(agent.activity ?? null);
        if (existing.status !== agent.status) {
          const wasOffline = existing.status === "offline";
          existing.status = agent.status;
          existing.view.setStatus(agent.status);
          // Presence flickers constantly while an agent works. Only a
          // departure is urgent; any other change waits for the current
          // routine (and any errand on top of it) to finish, so the agent
          // never resets mid-walk or mid-pour.
          if (agent.status === "offline" || wasOffline) {
            this.cancelSocial(existing, false);
            this.rebuildRoot(existing);
          } else {
            existing.brain.setNextRoot(this.rootFor(existing));
          }
        }
        return;
      }
      this.avatars.set(agent.id, this.createAvatar(agent, home, index));
    });

    for (const [id, avatar] of this.avatars) {
      if (present.has(id)) continue;
      this.dispose(avatar);
      this.avatars.delete(id);
    }
  }

  /**
   * Marks the agent the panel is open for. Engagement pauses the routine and
   * turns the agent toward the nearest human; null resumes it where it left
   * off.
   */
  engage(agentId: string | null) {
    if (this.engagedId === agentId) return;
    const previous = this.engagedId ? this.avatars.get(this.engagedId) : null;
    if (previous) {
      this.cancelSocial(previous);
      previous.brain.resume();
    }
    this.engagedId = agentId;
    const next = agentId ? this.avatars.get(agentId) : null;
    if (next) this.cancelSocial(next);
  }

  /** Visible agents, in world space, for proximity checks in the scene. */
  positions(): InteractableAgent[] {
    const list: InteractableAgent[] = [];
    for (const avatar of this.avatars.values()) {
      if (!this.isVisible(avatar)) continue;
      list.push({ id: avatar.id, x: avatar.x, y: avatar.y });
    }
    return list;
  }

  positionOf(agentId: string): InteractableAgent | null {
    const avatar = this.avatars.get(agentId);
    if (!avatar || !this.isVisible(avatar)) return null;
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
    const dtMs = Math.min(dtSeconds, 0.1) * 1000;
    this.scheduleSpawns(nowMs);

    for (const avatar of this.avatars.values()) {
      if (!avatar.active) continue;

      const view = avatar.view;
      const engaged = avatar.id === this.engagedId;
      let moving = false;

      if (engaged) {
        // The player is talking to this agent: hold its whole routine where
        // it is, face the player, and hand it back afterwards.
        this.cancelSocial(avatar);
        avatar.brain.suspend();
        avatar.path = [];
        avatar.goalKey = "";
        view.setWalking(false);
        view.setPose("stand");
        const human = nearestHuman(humans, avatar.x, avatar.y);
        if (human) {
          view.setDir(
            directionFor(human.x - avatar.x, human.y - avatar.y) ?? "down",
          );
        }
        avatar.chatUntil = nowMs + 400;
        view.setProp(avatar.brain.state().prop);
      } else if (avatar.visit || avatar.chatPartnerId) {
        // Walking over to a colleague, chatting, or walking back. The
        // suspended routine resumes seconds later, mid-burst.
        avatar.brain.suspend();
        moving = this.stepSocialAvatar(avatar, dtSeconds, nowMs);
        view.setWalking(moving);
        view.setPose("stand");
        view.setProp(avatar.brain.state().prop);
      } else {
        avatar.brain.resume();
        avatar.brain.update(dtMs);
        const state = avatar.brain.state();
        avatar.hidden = state.hidden;

        if (state.goal) {
          const station = this.stationOf(state.goal);
          if (station) {
            if (state.goal !== avatar.goalKey) {
              avatar.goalKey = state.goal;
              avatar.path = findPath(
                this.layout,
                { x: avatar.x, y: avatar.y },
                { x: station.x, y: station.y },
              );
            }
            moving = this.advance(avatar, dtSeconds, station);
            if (!moving) {
              avatar.goalKey = "";
              avatar.brain.arrived();
            }
          }
        } else {
          avatar.goalKey = "";
        }

        view.setWalking(moving);

        if (moving) {
          view.setPose("stand");
        } else {
          const human = nearestHuman(humans, avatar.x, avatar.y);
          view.setPose(state.pose);
          if (state.dir) view.setDir(state.dir);
          if (human && avatar.brain.currentAction()?.kind !== "walk") {
            if (nowMs >= avatar.nextChatAt) {
              avatar.chatUntil = nowMs + CHAT_MS;
              avatar.nextChatAt = nowMs + CHAT_MS + this.chatCooldown(avatar.id);
            }
          }
        }

        view.setProp(state.prop);
      }

      avatar.moving = moving;
      view.container.visible = avatar.active && !avatar.hidden;
      view.tick(nowMs);
      this.pulseScreen(avatar, nowMs);
    }

    this.stepSocialScheduler(nowMs);
    this.stepAgentChat(nowMs);

    for (const avatar of this.avatars.values()) {
      avatar.label.visible = this.isVisible(avatar);
      avatar.view.setBubble(this.bubbleFor(avatar, nowMs));
    }
  }

  destroy() {
    for (const avatar of this.avatars.values()) this.dispose(avatar);
    this.avatars.clear();
  }

  // -- avatar lifecycle ---------------------------------------------------

  private createAvatar(agent: OfficeAgentView, home: Station, index: number): Avatar {
    const view = createCharacter(this.pixi, {
      kind: "agent",
      seed: agent.id,
      status: agent.status,
    });
    view.container.position.set(this.layout.door.x, this.layout.door.y);
    view.container.visible = false;
    view.setDir("up");
    view.setCaption(agent.activity ?? null);
    view.container.eventMode = "static";
    view.container.cursor = "pointer";
    view.container.hitArea = new this.pixi.Rectangle(-8, -32, 16, 33);
    view.container.on("pointerdown", (event) => {
      event.stopPropagation();
      this.onAgentActivate(agent.id);
    });
    this.world.addChild(view.container);

    const label = this.createLabel(agent.name);
    label.position.set(0, -42);
    view.container.addChild(label);

    const env: RoutineEnv = {
      home: home.key,
      rng: createRng(`${agent.id}:${index}`),
      stations: this.stations,
      isFree: (key) => this.reservations.isFree(key),
    };
    const brain = new Behavior(agent.id, {
      stationExists: (key) => this.stationOf(key) !== null,
      isFree: (key) => this.reservations.isFree(key),
      tryReserve: (key, owner) => this.reservations.tryAcquire(key, owner),
      release: (key, owner) => this.reservations.release(key, owner),
    });

    return {
      id: agent.id,
      view,
      label,
      status: agent.status,
      home,
      env,
      brain,
      x: this.layout.door.x,
      y: this.layout.door.y,
      path: [],
      goalKey: "",
      moving: false,
      chatUntil: 0,
      nextChatAt: 0,
      emoteKind: null,
      emoteUntil: 0,
      active: false,
      spawnAt: null,
      hidden: false,
      screen: this.desksFor(index),
      visit: null,
      chatPartnerId: null,
      nextVisitAt: 0,
      nextBubbleAt: 0,
    };
  }

  /** Newcomers file through the doorway one at a time. */
  private scheduleSpawns(nowMs: number) {
    let waiting = 0;
    for (const avatar of this.avatars.values()) {
      if (avatar.active) continue;
      if (avatar.spawnAt === null) {
        avatar.spawnAt = nowMs + this.spawnQueue * SPAWN_STAGGER_MS;
        this.spawnQueue += 1;
      }
      waiting += 1;
      if (nowMs < avatar.spawnAt) continue;
      avatar.active = true;
      avatar.nextVisitAt =
        nowMs + FIRST_SOCIAL_DELAY_MS + (hashSeed(avatar.id) % 9_000);
      this.rebuildRoot(avatar);
    }
    if (waiting === 0) this.spawnQueue = 0;
  }

  private rebuildRoot(avatar: Avatar) {
    avatar.brain.setRoot(null);
    avatar.hidden = false;
    avatar.brain.setRoot(this.rootFor(avatar));
  }

  /** The loop an agent's presence implies, resolved lazily so it never
   *  picks a meeting seat or home station before it is actually needed. */
  private rootFor(avatar: Avatar): (() => Action[]) | null {
    switch (avatar.status) {
      case "working":
        return workRoot(avatar.env);
      case "queued":
        return () => {
          const seat = this.meetingSeats.find((item) =>
            this.reservations.isFree(item.key),
          );
          if (seat) return meetingRoutine(avatar.env, seat.key);
          return workRoot(avatar.env)();
        };
      case "idle":
        return idleRoot(avatar.env);
      case "offline": {
        let left = false;
        return () => {
          if (left) return [];
          left = true;
          return leaveRoutine("door");
        };
      }
    }
  }

  // -- per-frame helpers --------------------------------------------------

  private isVisible(avatar: Avatar) {
    return avatar.active && !avatar.hidden;
  }

  private stationOf(key: string): Station | null {
    return this.stations.find((station) => station.key === key) ?? null;
  }

  private desksFor(index: number): DeskScreen | null {
    return this.screens[index] ?? null;
  }

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
      } else {
        avatar.x = target.x;
        avatar.y = target.y;
        if (target.dir) avatar.view.setDir(target.dir);
      }
    }

    view.position.set(Math.round(avatar.x), Math.round(avatar.y));
    view.zIndex = Math.round(avatar.y);
    return moving;
  }

  /** Move along `avatar.path` with no station target, for social walks. */
  private advanceAlong(avatar: Avatar, dtSeconds: number): boolean {
    let remaining = WALK_SPEED * dtSeconds;
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

    avatar.view.container.position.set(Math.round(avatar.x), Math.round(avatar.y));
    avatar.view.container.zIndex = Math.round(avatar.y);
    return moving;
  }

  /** One frame of a walk-over conversation: approach, talk, then walk back. */
  private stepSocialAvatar(avatar: Avatar, dtSeconds: number, nowMs: number): boolean {
    // Host: stop working, turn toward the colleague, trade speech balloons.
    if (avatar.chatPartnerId) {
      avatar.path = [];
      const visitor = this.avatars.get(avatar.chatPartnerId);
      if (!visitor || !this.isVisible(visitor)) {
        avatar.chatPartnerId = null;
        return false;
      }
      const dir = directionFor(visitor.x - avatar.x, visitor.y - avatar.y);
      if (dir) avatar.view.setDir(dir);
      if (nowMs >= avatar.nextBubbleAt) {
        avatar.chatUntil = nowMs + BUBBLE_MS_MIN;
        avatar.nextBubbleAt =
          nowMs + BUBBLE_MS_MIN + (hashSeed(avatar.id) % BUBBLE_MS_SPREAD);
      }
      return false;
    }

    const visit = avatar.visit;
    if (!visit) return false;
    const target = this.avatars.get(visit.targetId);

    if (visit.phase === "talk") {
      if (!target || !this.isVisible(target) || target.chatPartnerId !== avatar.id) {
        this.startReturn(avatar, visit);
        return false;
      }
      const dir = directionFor(target.x - avatar.x, target.y - avatar.y);
      if (dir) avatar.view.setDir(dir);
      if (nowMs >= visit.nextBubbleAt) {
        avatar.chatUntil = nowMs + BUBBLE_MS_MIN;
        target.chatUntil = nowMs + BUBBLE_MS_MIN;
        visit.nextBubbleAt =
          nowMs +
          BUBBLE_MS_MIN +
          (hashSeed(`${avatar.id}:${target.id}:${Math.floor(nowMs / 5_000)}`) %
            BUBBLE_MS_SPREAD);
      }
      if (nowMs >= visit.until) this.startReturn(avatar, visit);
      return false;
    }

    // Walk over, or walk back after the chat.
    const moving = this.advanceAlong(avatar, dtSeconds);
    if (avatar.path.length > 0 || moving) return moving;

    if (visit.phase === "walk") {
      if (!target || !this.isVisible(target) || target.chatPartnerId !== avatar.id) {
        this.startReturn(avatar, visit);
        return false;
      }
      visit.phase = "talk";
      visit.until =
        nowMs +
        TALK_MS_MIN +
        (hashSeed(`${avatar.id}:${target.id}`) % TALK_MS_SPREAD);
      visit.nextBubbleAt = nowMs;
      // Open with a hand-off beat: an alert on the visitor and an ack on the
      // host, so the walk-over reads as bringing news, then as conversation.
      avatar.emoteKind = "alert";
      avatar.emoteUntil = nowMs + 900;
      target.emoteKind = "check";
      target.emoteUntil = nowMs + 1_300;
      return false;
    }

    // Back home: hand control back to the suspended routine.
    avatar.visit = null;
    avatar.brain.resume();
    return false;
  }

  /** Ends the chat and walks the visitor back to its own desk. */
  private startReturn(avatar: Avatar, visit: VisitState) {
    const target = this.avatars.get(visit.targetId);
    if (target?.chatPartnerId === avatar.id) {
      target.chatPartnerId = null;
      target.nextBubbleAt = 0;
    }
    visit.phase = "return";
    avatar.path = findPath(
      this.layout,
      { x: avatar.x, y: avatar.y },
      { x: avatar.home.x, y: avatar.home.y },
    );
  }

  /**
   * Occasionally picks two agents at their desks and sends one over to chat.
   * Conversations are coordinated as a pair so both turn toward each other and
   * neither restarts its current task.
   */
  private stepSocialScheduler(nowMs: number) {
    if (this.nextSocialAt === 0) this.nextSocialAt = nowMs + 6_000;
    if (nowMs < this.nextSocialAt) return;

    const ready = [...this.avatars.values()].filter((avatar) =>
      this.canSocialize(avatar, nowMs),
    );
    const pair = pickVisitPair(ready, SOCIAL_RADIUS);
    const visitor = pair ? this.avatars.get(pair.visitor.id) : null;
    const host = pair ? this.avatars.get(pair.host.id) : null;
    if (!visitor || !host) {
      this.nextSocialAt = nowMs + 4_000;
      return;
    }

    const slot = nearestVisitSlot(
      this.layout,
      { x: host.x, y: host.y },
      { x: visitor.x, y: visitor.y },
    );
    const path = slot
      ? findPath(this.layout, { x: visitor.x, y: visitor.y }, slot)
      : [];
    if (!slot || path.length === 0) {
      this.nextSocialAt = nowMs + 4_000;
      return;
    }

    visitor.visit = {
      targetId: host.id,
      phase: "walk",
      until: 0,
      nextBubbleAt: 0,
    };
    visitor.path = path;
    visitor.goalKey = "";
    host.chatPartnerId = visitor.id;
    host.nextBubbleAt = 0;

    this.nextSocialAt =
      nowMs +
      SOCIAL_COOLDOWN_MS +
      (hashSeed(visitor.id + host.id) % SOCIAL_COOLDOWN_SPREAD_MS);
    visitor.nextVisitAt = nowMs + 35_000 + (hashSeed(visitor.id) % 30_000);
    host.nextVisitAt = nowMs + 35_000 + (hashSeed(host.id) % 30_000);
  }

  private canSocialize(avatar: Avatar, nowMs: number): boolean {
    if (!this.isVisible(avatar)) return false;
    if (avatar.id === this.engagedId) return false;
    if (avatar.status !== "working" && avatar.status !== "idle") return false;
    if (avatar.visit || avatar.chatPartnerId) return false;
    if (avatar.moving || avatar.path.length > 0) return false;
    if (nowMs < avatar.nextVisitAt) return false;
    // Only agents at their own desk take part. The visitor walks home to a
    // spot that matches the routine it suspended, so it resumes in place.
    if (Math.hypot(avatar.x - avatar.home.x, avatar.y - avatar.home.y) > TILE) {
      return false;
    }
    const action = avatar.brain.currentAction();
    return action !== null && action.kind !== "walk";
  }

  /**
   * Ends a walk-over or conversation. An interrupted visitor goes home first
   * and only then resumes its routine, so nobody is left typing in a hallway.
   * `sendHome = false` is for agents leaving the office entirely.
   */
  private cancelSocial(avatar: Avatar, sendHome = true) {
    if (avatar.visit) {
      const visit = avatar.visit;
      const target = this.avatars.get(visit.targetId);
      if (target?.chatPartnerId === avatar.id) {
        target.chatPartnerId = null;
        target.nextBubbleAt = 0;
      }
      if (!sendHome) {
        avatar.visit = null;
        avatar.path = [];
      } else if (visit.phase !== "return") {
        visit.phase = "return";
        avatar.path = findPath(
          this.layout,
          { x: avatar.x, y: avatar.y },
          { x: avatar.home.x, y: avatar.home.y },
        );
      }
    }
    if (avatar.chatPartnerId) {
      const visitor = this.avatars.get(avatar.chatPartnerId);
      if (visitor?.visit?.targetId === avatar.id) {
        // The visitor is still on the clock: it walks home and resumes.
        this.cancelSocial(visitor);
      }
      avatar.chatPartnerId = null;
      avatar.nextBubbleAt = 0;
    }
  }

  private pulseScreen(avatar: Avatar, nowMs: number) {
    if (!avatar.screen) return;
    const atHome =
      avatar.status === "working" &&
      Math.hypot(avatar.x - avatar.home.x, avatar.y - avatar.home.y) < 2;
    avatar.screen.setOn(atHome, nowMs);
  }


  private bubbleFor(avatar: Avatar, nowMs: number): BubbleKind | null {
    if (!this.isVisible(avatar)) return null;
    if (nowMs < avatar.emoteUntil && avatar.emoteKind) return avatar.emoteKind;
    if (avatar.status === "offline" && avatar.brain.currentAction()?.kind === "walk") {
      return null;
    }
    if (avatar.id === this.engagedId) return "chat";
    const state = avatar.brain.state();
    if (state.bubble) return state.bubble;
    if (nowMs < avatar.chatUntil) return "chat";
    if (avatar.status === "queued" && !avatar.moving) return "dots";
    return null;
  }

  /** Two agents standing together chat, which is the "talking shop" beat. */
  private stepAgentChat(nowMs: number) {
    const list = [...this.avatars.values()].filter(
      (avatar) => !avatar.moving && this.isVisible(avatar),
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

  private dispose(avatar: Avatar) {
    this.cancelSocial(avatar, false);
    this.reservations.releaseAll(avatar.id);
    avatar.brain.stop();
    avatar.view.container.destroy({ children: true });
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
