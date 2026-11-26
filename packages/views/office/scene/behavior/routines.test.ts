// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildOfficeLayout } from "../layout";
import { Behavior } from "./behavior";
import { createRng } from "./rng";
import { Reservations } from "./reservations";
import {
  IDLE_ERRANDS,
  WORK_ERRANDS,
  idleRoot,
  leaveRoutine,
  meetingRoutine,
  pickErrand,
  workRoot,
  type RoutineEnv,
} from "./routines";

const layout = buildOfficeLayout();

function env(seed = "agent-1"): RoutineEnv & { reservations: Reservations } {
  const reservations = new Reservations(layout.stations);
  return {
    home: "desk:0",
    rng: createRng(seed),
    stations: layout.stations,
    isFree: (key) => reservations.isFree(key),
    reservations,
  };
}

function hooksFor(context: RoutineEnv) {
  return {
    stationExists: (key: string) => layout.stations.some((s) => s.key === key),
    isFree: (key: string) => context.isFree(key),
    tryReserve: () => true,
    release: () => {},
  };
}

describe("workRoot", () => {
  it("starts every burst at the desk with a long typing pose", () => {
    const steps = workRoot(env())();
    expect(steps[0]).toEqual({ kind: "show" });
    expect(steps.some((step) => step.kind === "walk" && step.station === "desk:0")).toBe(true);
    const typing = steps.filter(
      (step) => step.kind === "pose" && step.pose === "type",
    );
    expect(typing).toHaveLength(2);
    expect(typing[0]).toMatchObject({
      kind: "pose",
      pose: "type",
      ms: expect.any(Number),
    });
    expect(typing[0]!.kind === "pose" ? typing[0]!.ms : 0).toBeGreaterThanOrEqual(18_000);
    // The burst ends back in the chair, and the loop restarts after that.
    expect(steps[steps.length - 1]).toMatchObject({ pose: "sit" });
  });

  it("keeps hydration and food breaks in the work rotation", () => {
    expect(WORK_ERRANDS).toEqual(
      expect.arrayContaining(["coffee", "water", "snack"]),
    );
    expect(IDLE_ERRANDS).toEqual(
      expect.arrayContaining(["sofa", "beanbag", "kitchen-table"]),
    );
  });

  it("carries the cup back to the desk before drinking it", () => {
    const errand = pickErrand(env("drink"), ["coffee"]);
    expect(errand).not.toBeNull();
    const drinkIndex = errand!.findIndex((step) => step.kind === "pose" && step.pose === "drink");
    expect(drinkIndex).toBeGreaterThan(0);
    // The drink happens at the desk: a walk home precedes it.
    expect(
      errand!
        .slice(0, drinkIndex)
        .some((step) => step.kind === "walk" && step.station === "desk:0"),
    ).toBe(true);
    expect(errand![errand!.length - 1]).toEqual({ kind: "prop", prop: "none" });
  });

  it("is deterministic for a given seed", () => {
    const first = workRoot(env("seed"))().map((step) => step.kind);
    const second = workRoot(env("seed"))().map((step) => step.kind);
    expect(first).toEqual(second);
  });

  it("pushes an errand that runs to the machine and back", () => {
    const context = env("errand");
    const brain = new Behavior("agent-1", hooksFor(context));
    const errand = pickErrand(context);
    expect(errand).not.toBeNull();
    const stationKey = (errand![0] as { station?: string }).station;
    expect(stationKey).toBeDefined();
    // The errand ends by walking home, so the agent never strands itself.
    const lastWalk = [...errand!].reverse().find((step) => step.kind === "walk");
    expect(lastWalk).toMatchObject({ station: "desk:0" });

    brain.setRoot(() => []);
    brain.pushRoutine(errand!);
    brain.update(16);
    expect(brain.state().goal).toBe(stationKey);
  });
});

describe("pickErrand", () => {
  it("skips stations someone else holds and its own desk", () => {
    const context = env("busy");
    const free = pickErrand(context);
    expect(free).not.toBeNull();
    const reserved = (free![0] as { station: string }).station;
    context.reservations.tryAcquire(reserved, "other");

    const next = pickErrand(context);
    const nextKey = next ? (next[0] as { station: string }).station : null;
    expect(nextKey).not.toBe(reserved);
    expect(nextKey).not.toBe("desk:0");
  });

  it("returns null when every spot is taken", () => {
    const context = env("full");
    for (const station of context.stations) {
      if (station.key === "desk:0") continue;
      for (let seat = 0; seat < station.capacity; seat++) {
        context.reservations.tryAcquire(station.key, `other-${seat}`);
      }
    }
    expect(pickErrand(context)).toBeNull();
  });
});

describe("idleRoot", () => {
  function forced(firstChance: boolean): RoutineEnv {
    const context = env("forced");
    return { ...context, rng: { ...context.rng, chance: () => firstChance } };
  }

  it("walks back to the desk and reads when staying in", () => {
    const steps = idleRoot(forced(false))();
    expect(steps.some((step) => step.kind === "walk" && step.station === "desk:0")).toBe(true);
    expect(steps.some((step) => step.kind === "pose" && step.pose === "read")).toBe(true);
    expect(steps[steps.length - 1]).toMatchObject({ pose: "sit" });
  });

  it("takes a real break occasionally and still walks", () => {
    const steps = idleRoot(forced(true))();
    expect(steps[0]).toEqual({ kind: "show" });
    expect(steps.some((step) => step.kind === "walk")).toBe(true);
  });
});

describe("meetingRoutine", () => {
  it("reserves its seat and releases it at the end", () => {
    const steps = meetingRoutine(env(), "meeting:n:6");
    expect(steps[0]).toEqual({ kind: "reserve", station: "meeting:n:6" });
    expect(steps[steps.length - 1]).toEqual({ kind: "release" });
    expect(steps.some((step) => step.kind === "walk")).toBe(true);
  });
});

describe("leaveRoutine", () => {
  it("walks to the door and hides", () => {
    const steps = leaveRoutine("door");
    expect(steps[0]).toEqual({ kind: "walk", station: "door" });
    expect(steps.some((step) => step.kind === "hide")).toBe(true);
  });
});
