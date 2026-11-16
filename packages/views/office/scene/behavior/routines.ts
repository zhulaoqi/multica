import type { Station, StationKind } from "../layout";
import type { Action } from "./behavior";
import type { Rng } from "./rng";

/**
 * The office's daily life, written as action list builders.
 *
 * A root routine is a loop: `workRoot` returns the next typing burst and the
 * behaviour runtime asks for another one when the previous finishes. Errands
 * are pushed as child routines from inside a burst, which is what lets an
 * agent leave mid-work and come back to the same task.
 *
 * Every errand is a round trip: reserve the station, walk over, use it while
 * the prop animates, carry the result back, finish the gesture at the desk,
 * and only then let the interrupted burst resume. Nothing resets the agent's
 * day; it is one continuous thread.
 */
export interface RoutineEnv {
  /** The agent's own desk station. */
  home: string;
  rng: Rng;
  stations: readonly Station[];
  /** Whether another agent is already using the station. */
  isFree(key: string): boolean;
}

/** Errand stations, in the order an agent prefers them. */
const ERRAND_KINDS: StationKind[] = [
  "coffee",
  "water",
  "snack",
  "printer",
  "sofa",
  "beanbag",
  "kitchen-table",
  "plant",
  "server",
];

/** While heads-down: quick breaks that keep them near their desk. */
const WORK_ERRANDS: StationKind[] = ["coffee", "water", "snack", "printer", "server", "plant"];

/** While idle: longer trips to the lounge and pantry. */
const IDLE_ERRANDS: StationKind[] = [
  "sofa",
  "beanbag",
  "kitchen-table",
  "water",
  "coffee",
  "plant",
];

function station(env: RoutineEnv, key: string): Station | null {
  return env.stations.find((item) => item.key === key) ?? null;
}

function goHome(env: RoutineEnv, pose: "sit" | "stand" = "sit"): Action[] {
  const home = station(env, env.home);
  return [
    { kind: "walk", station: env.home },
    { kind: "face", dir: home?.dir ?? "down" },
    { kind: "pose", pose: home?.seat ? pose : "stand", ms: env.rng.int(300, 700) },
  ];
}

/** Walk back to the desk still holding the cup, then drink it sitting down. */
function drinkAtDesk(env: RoutineEnv, ms: number): Action[] {
  const home = station(env, env.home);
  return [
    { kind: "walk", station: env.home },
    { kind: "face", dir: home?.dir ?? "down" },
    { kind: "pose", pose: home?.seat ? "sit" : "stand", ms: 350 },
    { kind: "pose", pose: "drink", ms },
    { kind: "pose", pose: home?.seat ? "sit" : "stand", ms: env.rng.int(500, 1_000) },
    { kind: "prop", prop: "none" },
  ];
}

function coffeeErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "prop", prop: "cup" },
    { kind: "pose", pose: "pour", ms: 1_600 },
    { kind: "say", bubble: "check", ms: 700 },
    { kind: "release" },
    ...drinkAtDesk(env, 1_300),
  ];
}

function waterErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "prop", prop: "cup" },
    { kind: "pose", pose: "pour", ms: 1_200 },
    { kind: "release" },
    ...drinkAtDesk(env, 900),
  ];
}

/** Grab a can from the vending machine and carry it back to the desk. */
function snackErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "wait", ms: 900 },
    { kind: "prop", prop: "cup" },
    { kind: "say", bubble: "check", ms: 500 },
    { kind: "release" },
    ...drinkAtDesk(env, 800),
  ];
}

function printerErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "wait", ms: 1_200 },
    { kind: "prop", prop: "paper" },
    { kind: "say", bubble: "check", ms: 600 },
    { kind: "release" },
    { kind: "walk", station: env.home },
    { kind: "face", dir: station(env, env.home)?.dir ?? "down" },
    { kind: "pose", pose: station(env, env.home)?.seat ? "sit" : "stand", ms: 400 },
    { kind: "prop", prop: "none" },
  ];
}

/** Sit down somewhere soft, read or doze, and come back to the desk. */
function restErrand(env: RoutineEnv, spot: Station): Action[] {
  const sleeping = spot.kind === "beanbag" || env.rng.chance(0.25);
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "say", bubble: "dots", ms: 700 },
    {
      kind: "pose",
      pose: sleeping ? "sleep" : "read",
      ms: env.rng.int(7_000, 13_000),
    },
    { kind: "pose", pose: "sit", ms: env.rng.int(700, 1_400) },
    { kind: "release" },
    ...goHome(env),
  ];
}

/** A break at the pantry table: drink something, watch the room. */
function pantryErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "prop", prop: "cup" },
    { kind: "pose", pose: "drink", ms: 900 },
    { kind: "pose", pose: "sit", ms: env.rng.int(3_000, 6_000) },
    { kind: "say", bubble: "chat", ms: 1_200 },
    { kind: "pose", pose: "sit", ms: env.rng.int(2_000, 4_000) },
    { kind: "prop", prop: "none" },
    { kind: "release" },
    ...goHome(env),
  ];
}

function plantErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "pose", pose: "pour", ms: 1_100 },
    { kind: "release" },
    ...goHome(env),
  ];
}

function serverErrand(env: RoutineEnv, spot: Station): Action[] {
  return [
    { kind: "reserve", station: spot.key },
    { kind: "walk", station: spot.key },
    { kind: "face", dir: spot.dir },
    { kind: "say", bubble: "dots", ms: 1_600 },
    { kind: "pose", pose: "read", ms: 1_400 },
    { kind: "release" },
    ...goHome(env),
  ];
}

function errandFor(env: RoutineEnv, spot: Station): Action[] {
  switch (spot.kind) {
    case "coffee":
      return coffeeErrand(env, spot);
    case "water":
      return waterErrand(env, spot);
    case "snack":
      return snackErrand(env, spot);
    case "printer":
      return printerErrand(env, spot);
    case "sofa":
    case "beanbag":
    case "reception":
      return restErrand(env, spot);
    case "kitchen-table":
      return pantryErrand(env, spot);
    case "plant":
      return plantErrand(env, spot);
    case "server":
      return serverErrand(env, spot);
    default:
      return goHome(env);
  }
}

/** A random free errand, or null when every spot is taken. */
export function pickErrand(
  env: RoutineEnv,
  kinds: readonly StationKind[] = ERRAND_KINDS,
): Action[] | null {
  const candidates = env.stations.filter(
    (spot) =>
      kinds.includes(spot.kind) &&
      spot.key !== env.home &&
      env.isFree(spot.key),
  );
  const spot = env.rng.pick(candidates);
  if (!spot) return null;
  return errandFor(env, spot);
}

/** Working agents type in bursts and sometimes step out mid-burst. */
export function workRoot(env: RoutineEnv): () => Action[] {
  return () => {
    const steps: Action[] = [
      { kind: "show" },
      { kind: "walk", station: env.home },
      { kind: "face", dir: "down" },
      { kind: "pose", pose: "type", ms: env.rng.int(6_000, 12_000) },
      {
        kind: "fn",
        run: (brain) => {
          // Leave in the middle of the burst: the typing frame stays on the
          // stack and resumes when the errand returns.
          if (!env.rng.chance(0.55)) return;
          const errand = pickErrand(env, WORK_ERRANDS);
          if (errand) brain.pushRoutine(errand);
        },
      },
      { kind: "pose", pose: "type", ms: env.rng.int(4_000, 8_000) },
      { kind: "pose", pose: "sit", ms: env.rng.int(600, 1_600) },
    ];
    return steps;
  };
}

/** Queued agents gather around the meeting table and read the shared board. */
export function meetingRoutine(env: RoutineEnv, seat: string): Action[] {
  const spot = station(env, seat);
  return [
    { kind: "reserve", station: seat },
    { kind: "walk", station: seat },
    { kind: "face", dir: spot?.dir ?? "down" },
    { kind: "say", bubble: "dots", ms: 900 },
    { kind: "pose", pose: "read", ms: env.rng.int(4_000, 7_000) },
    { kind: "pose", pose: "sit", ms: env.rng.int(1_200, 2_400) },
    { kind: "say", bubble: "chat", ms: 1_300 },
    { kind: "pose", pose: "read", ms: env.rng.int(3_000, 6_000) },
    { kind: "pose", pose: "sit", ms: env.rng.int(1_500, 3_000) },
    { kind: "release" },
  ];
}

export function meetingRoot(env: RoutineEnv, seat: string): () => Action[] {
  return () => meetingRoutine(env, seat);
}

/** Idle agents wander to the soft corners of the office and doze at their desk. */
export function idleRoot(env: RoutineEnv): () => Action[] {
  return () => {
    const errand = pickErrand(env, IDLE_ERRANDS);
    if (errand) return [{ kind: "show" }, ...errand];
    return [
      { kind: "show" },
      { kind: "walk", station: env.home },
      { kind: "face", dir: "down" },
      { kind: "pose", pose: "sleep", ms: env.rng.int(4_000, 8_000) },
      { kind: "say", bubble: "sleep", ms: 1_400 },
    ];
  };
}

/** Walks to the doorway, waves, and disappears until it comes back. */
export function leaveRoutine(door: string): Action[] {
  return [
    { kind: "walk", station: door },
    { kind: "face", dir: "down" },
    { kind: "say", bubble: "sleep", ms: 600 },
    { kind: "hide" },
    { kind: "release" },
  ];
}
