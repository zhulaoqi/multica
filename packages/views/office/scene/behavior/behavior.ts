import type { OfficeDir } from "@multica/core/office";
import type { BubbleKind, CharacterPose, CharacterProp } from "../characters";

/**
 * Behaviour runtime: a stack of routines made of actions.
 *
 * The stack is what makes errands feel continuous. An agent typing at its desk
 * is inside a routine frame with time left on the current `pose` action; when
 * the routine decides to fetch coffee it *pushes* the coffee routine. The
 * coffee frame runs to completion, pops, and the typing frame resumes with the
 * exact remaining milliseconds. Nothing restarts, and the character walks back
 * to the desk because the coffee routine ends with the walk home.
 */
export type Action =
  | { kind: "walk"; station: string }
  | { kind: "face"; dir: OfficeDir }
  | { kind: "pose"; pose: CharacterPose; ms: number }
  | { kind: "wait"; ms: number }
  | { kind: "say"; bubble: BubbleKind; ms: number }
  | { kind: "prop"; prop: CharacterProp }
  | { kind: "reserve"; station: string }
  | { kind: "release" }
  | { kind: "hide" }
  | { kind: "show" }
  | { kind: "fn"; run: (brain: Behavior) => void };

export interface BehaviorState {
  /** Station the character is walking to, or null when standing still. */
  goal: string | null;
  dir: OfficeDir | null;
  pose: CharacterPose;
  bubble: BubbleKind | null;
  prop: CharacterProp;
  hidden: boolean;
}

export interface BehaviorHooks {
  stationExists(station: string): boolean;
  /** Whether the station has room for another character right now. */
  isFree(station: string): boolean;
  tryReserve(station: string, owner: string): boolean;
  release(station: string, owner: string): void;
}

/** How long an agent stands aside before retrying a busy station. */
export const RESERVE_RETRY_MS = 900;

interface Frame {
  steps: Action[];
  index: number;
  started: boolean;
  remaining: number;
}

const IDLE_STATE: BehaviorState = {
  goal: null,
  dir: null,
  pose: "stand",
  bubble: null,
  prop: "none",
  hidden: false,
};

export class Behavior {
  private readonly owner: string;
  private readonly hooks: BehaviorHooks;
  private frames: Frame[] = [];
  private root: (() => Action[]) | null = null;
  private nextRoot: (() => Action[]) | null = null;
  private snapshot: BehaviorState = { ...IDLE_STATE };
  private suspended = false;
  private clock = 0;
  private reserving = false;
  private retryAt = 0;
  private reserved: string | null = null;

  constructor(owner: string, hooks: BehaviorHooks) {
    this.owner = owner;
    this.hooks = hooks;
  }

  get elapsedMs(): number {
    return this.clock;
  }

  get isBusy(): boolean {
    return this.frames.length > 0;
  }

  state(): BehaviorState {
    return { ...this.snapshot };
  }

  /** Replaces the whole behaviour, e.g. when presence changes. */
  setRoot(factory: (() => Action[]) | null) {
    this.releaseStation();
    this.frames = [];
    this.root = factory;
    this.nextRoot = null;
    this.snapshot.goal = null;
    this.snapshot.bubble = null;
    this.reserving = false;
    if (factory) this.startRoot();
  }

  /**
   * Swaps the loop once the current routine finishes. The in-flight action,
   * and any errand pushed on top of it, run to completion first — a status
   * change must not teleport an agent out of the coffee queue or restart its
   * typing burst.
   */
  setNextRoot(factory: (() => Action[]) | null) {
    this.nextRoot = factory;
  }

  /** Interrupts the current routine and resumes it when the child finishes. */
  pushRoutine(steps: Action[]) {
    if (steps.length === 0) return;
    this.frames.push({ steps, index: 0, started: false, remaining: 0 });
  }

  currentAction(): Action | null {
    const frame = this.frames[this.frames.length - 1];
    if (!frame) return null;
    return frame.steps[frame.index] ?? null;
  }

  /** Engagement pauses the routine without losing the remaining action time. */
  suspend() {
    this.suspended = true;
  }

  resume() {
    this.suspended = false;
  }

  /** The scene reports that a `walk` action reached its station. */
  arrived() {
    const frame = this.frames[this.frames.length - 1];
    if (!frame || frame.steps[frame.index]?.kind !== "walk") return;
    this.snapshot.goal = null;
    frame.started = false;
    frame.index += 1;
  }

  /** Freezes the character exactly where it is, e.g. when it goes offline. */
  stop() {
    this.releaseStation();
    this.frames = [];
    this.root = null;
    this.nextRoot = null;
    this.snapshot = { ...this.snapshot, goal: null, bubble: null };
  }

  update(dtMs: number) {
    this.clock += dtMs;
    if (this.suspended) return;
    if (this.reserving) {
      if (this.clock < this.retryAt) return;
      if (!this.acquireForCurrent()) return;
    }

    let budget = dtMs;
    for (let guard = 0; guard < 128; guard++) {
      const frame = this.frames[this.frames.length - 1];
      if (!frame) {
        if (!this.startRoot()) return;
        continue;
      }
      const action = frame.steps[frame.index];
      if (!action) {
        this.finishFrame();
        continue;
      }

      if (!frame.started) {
        frame.started = true;
        const duration = this.begin(action);
        if (duration === null) {
          this.finishAction(action, frame);
          continue;
        }
        budget = dtMs;
        if (duration === Number.POSITIVE_INFINITY) return;
        frame.remaining = duration;
      }

      if (budget <= 0) return;
      const used = Math.min(budget, frame.remaining);
      frame.remaining -= used;
      budget -= used;
      if (frame.remaining > 0) return;
      this.finishAction(action, frame);
      if (budget <= 0) return;
    }
  }

  // -- internals ---------------------------------------------------------

  /** Returns the duration in ms, null for an instant action, or Infinity
   *  for one that finishes on an external signal. */
  private begin(action: Action): number | null {
    switch (action.kind) {
      case "walk":
        if (!this.hooks.stationExists(action.station)) return null;
        this.snapshot.goal = action.station;
        return Number.POSITIVE_INFINITY;
      case "face":
        this.snapshot.dir = action.dir;
        return null;
      case "pose":
        this.snapshot.pose = action.pose;
        return Math.max(0, action.ms);
      case "wait":
        return Math.max(0, action.ms);
      case "say":
        this.snapshot.bubble = action.bubble;
        return Math.max(0, action.ms);
      case "prop":
        this.snapshot.prop = action.prop;
        return null;
      case "reserve":
        if (this.hooks.tryReserve(action.station, this.owner)) {
          this.reserved = action.station;
          return null;
        }
        this.reserving = true;
        this.retryAt = this.clock + RESERVE_RETRY_MS;
        return Number.POSITIVE_INFINITY;
      case "release":
        this.releaseStation();
        return null;
      case "hide":
        this.snapshot.hidden = true;
        this.snapshot.goal = null;
        return null;
      case "show":
        this.snapshot.hidden = false;
        return null;
      case "fn":
        action.run(this);
        return null;
    }
  }

  private finishAction(action: Action, frame: Frame) {
    if (action.kind === "say") this.snapshot.bubble = null;
    if (action.kind === "pose" && action.pose === "sleep") this.snapshot.pose = "stand";
    frame.started = false;
    frame.index += 1;
  }

  private finishFrame() {
    this.frames.pop();
    this.releaseStation();
  }

  private startRoot(): boolean {
    if (this.nextRoot) {
      this.root = this.nextRoot;
      this.nextRoot = null;
    }
    if (!this.root) return false;
    const steps = this.root();
    if (steps.length === 0) {
      this.root = null;
      return false;
    }
    this.frames.push({ steps, index: 0, started: false, remaining: 0 });
    return true;
  }

  private acquireForCurrent(): boolean {
    const action = this.currentAction();
    if (!action || action.kind !== "reserve") {
      this.reserving = false;
      return true;
    }
    if (!this.hooks.tryReserve(action.station, this.owner)) {
      this.retryAt = this.clock + RESERVE_RETRY_MS;
      return false;
    }
    this.reserved = action.station;
    this.reserving = false;
    const frame = this.frames[this.frames.length - 1]!;
    frame.started = false;
    frame.index += 1;
    return true;
  }

  private releaseStation() {
    if (this.reserved) {
      this.hooks.release(this.reserved, this.owner);
      this.reserved = null;
    }
  }
}
