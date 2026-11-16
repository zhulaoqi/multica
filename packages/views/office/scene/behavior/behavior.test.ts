// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { Behavior, RESERVE_RETRY_MS, type Action, type BehaviorHooks } from "./behavior";
import { Reservations } from "./reservations";

function hooks(overrides: Partial<BehaviorHooks> = {}): BehaviorHooks {
  return {
    stationExists: () => true,
    isFree: () => true,
    tryReserve: () => true,
    release: vi.fn(),
    ...overrides,
  };
}

/** Drives a walk action to completion, as the scene does on arrival. */
function finishWalks(brain: Behavior) {
  let guard = 0;
  while (brain.currentAction()?.kind === "walk" && guard++ < 20) {
    brain.arrived();
  }
}

describe("Behavior", () => {
  it("runs actions in order and spends their time", () => {
    const brain = new Behavior("a", hooks());
    brain.setRoot(() => [
      { kind: "pose", pose: "type", ms: 100 },
      { kind: "pose", pose: "sit", ms: 100 },
    ]);

    brain.update(60);
    expect(brain.state().pose).toBe("type");

    brain.update(60);
    expect(brain.state().pose).toBe("sit");

    brain.update(100);
    // The root loop restarts, so a fresh burst begins at the first pose.
    expect(brain.state().pose).toBe("type");
  });

  it("resumes the parent routine after an errand finishes", () => {
    const brain = new Behavior("a", hooks());
    const errand: Action[] = [
      { kind: "walk", station: "coffee" },
      { kind: "pose", pose: "pour", ms: 200 },
      { kind: "walk", station: "desk" },
    ];
    brain.setRoot(() => [
      { kind: "pose", pose: "type", ms: 1_000 },
      {
        kind: "fn",
        run: (self) => {
          self.pushRoutine(errand);
        },
      },
      { kind: "pose", pose: "type", ms: 500 },
      { kind: "pose", pose: "sit", ms: 300 },
    ]);

    // Finish the first typing burst; the errand walks to the coffee machine.
    brain.update(1_000);
    brain.update(16);
    expect(brain.state().goal).toBe("coffee");

    finishWalks(brain);
    brain.update(16);
    expect(brain.state().pose).toBe("pour");
    brain.update(200);
    finishWalks(brain);

    // Errand done: the second typing burst resumes where it left off.
    brain.update(16);
    expect(brain.state().pose).toBe("type");
    brain.update(483);
    expect(brain.state().pose).toBe("type");
    brain.update(2);
    expect(brain.state().pose).toBe("sit");
  });

  it("keeps the remaining time while suspended", () => {
    const brain = new Behavior("a", hooks());
    brain.setRoot(() => [
      { kind: "pose", pose: "type", ms: 1_000 },
      { kind: "pose", pose: "sit", ms: 300 },
    ]);
    brain.update(400);
    brain.suspend();
    brain.update(5_000);
    expect(brain.state().pose).toBe("type");
    brain.resume();
    brain.update(599);
    expect(brain.state().pose).toBe("type");
    brain.update(2);
    expect(brain.state().pose).toBe("sit");
  });

  it("waits for a busy station, then reserves it", () => {
    const reservations = new Reservations([{ key: "coffee", capacity: 1 }]);
    reservations.tryAcquire("coffee", "someone-else");
    const brain = new Behavior("a", {
      stationExists: () => true,
      isFree: () => reservations.isFree("coffee"),
      tryReserve: (key, owner) => reservations.tryAcquire(key, owner),
      release: (key, owner) => reservations.release(key, owner),
    });
    let started = false;
    brain.setRoot(() => {
      if (started) return [];
      started = true;
      return [
        { kind: "reserve", station: "coffee" },
        { kind: "pose", pose: "pour", ms: 300 },
      ];
    });

    brain.update(16);
    expect(brain.state().pose).toBe("stand");
    brain.update(RESERVE_RETRY_MS + 10);
    expect(brain.state().pose).toBe("stand");

    reservations.release("coffee", "someone-else");
    brain.update(RESERVE_RETRY_MS + 10);
    expect(brain.state().pose).toBe("pour");
    // The frame also releases on completion, so the station frees up again.
    brain.update(400);
    expect(reservations.isFree("coffee")).toBe(true);
  });

  it("replaces the stack on a status change and frees its reservation", () => {
    const released = vi.fn();
    const brain = new Behavior("a", hooks({ release: released }));
    brain.setRoot(() => [
      { kind: "reserve", station: "sofa" },
      { kind: "pose", pose: "sit", ms: 5_000 },
    ]);
    brain.update(16);
    expect(brain.state().pose).toBe("sit");

    brain.setRoot(() => [{ kind: "pose", pose: "type", ms: 5_000 }]);
    expect(released).toHaveBeenCalledWith("sofa", "a");
    brain.update(16);
    expect(brain.state().pose).toBe("type");
  });

  it("defers a root swap until the current routine finishes", () => {
    const brain = new Behavior("a", hooks());
    let bursts = 0;
    brain.setRoot(() => {
      bursts += 1;
      return [
        { kind: "pose", pose: "type", ms: 1_000 },
        { kind: "pose", pose: "sit", ms: 100 },
      ];
    });
    brain.update(400);
    expect(brain.state().pose).toBe("type");

    brain.setNextRoot(() => [{ kind: "pose", pose: "read", ms: 1_000 }]);
    brain.update(300);
    // The typing burst keeps its remaining time instead of restarting.
    expect(brain.state().pose).toBe("type");
    expect(bursts).toBe(1);

    brain.update(400);
    expect(brain.state().pose).toBe("read");
    expect(bursts).toBe(1);
  });

  it("lets an errand pushed before a pending root swap finish first", () => {
    const brain = new Behavior("a", hooks());
    brain.setRoot(() => [
      { kind: "pose", pose: "type", ms: 1_000 },
      {
        kind: "fn",
        run: (self) => self.pushRoutine([{ kind: "pose", pose: "pour", ms: 500 }]),
      },
      { kind: "pose", pose: "sit", ms: 100 },
    ]);
    brain.setNextRoot(() => [{ kind: "pose", pose: "read", ms: 1_000 }]);

    brain.update(1_000);
    brain.update(16);
    expect(brain.state().pose).toBe("pour");
    brain.update(600);
    expect(brain.state().pose).toBe("read");
  });

  it("hides and shows the character", () => {
    const brain = new Behavior("a", hooks());
    brain.setRoot(() => [
      { kind: "hide" },
      { kind: "wait", ms: 100 },
    ]);
    brain.update(16);
    expect(brain.state().hidden).toBe(true);
  });
});
