// @vitest-environment node

import { describe, expect, it } from "vitest";

import {
  isWalkableTile,
  tileAtWorld,
  buildOfficeLayout,
} from "./layout";
import { nearestAgent, nearestVisitSlot, visitSlots } from "./interactions";

const layout = buildOfficeLayout();

describe("visitSlots", () => {
  it("skips furniture next to the agent", () => {
    const desk = layout.agents[0]!;
    const slots = visitSlots(layout, desk);
    const { tx, ty } = tileAtWorld(desk.x, desk.y);

    // The tile below the desk agent is the desk itself.
    expect(isWalkableTile(layout, tx, ty + 1)).toBe(false);
    expect(slots.some((slot) => tileAtWorld(slot.x, slot.y).ty === ty + 1)).toBe(false);
    expect(slots.length).toBeGreaterThan(0);
  });

  it("only returns walkable tiles inside the room", () => {
    const agents = [...layout.agents, ...layout.overflowSpots];

    for (const agent of agents) {
      for (const slot of visitSlots(layout, agent)) {
        const { tx, ty } = tileAtWorld(slot.x, slot.y);
        expect(isWalkableTile(layout, tx, ty)).toBe(true);
      }
    }
  });

  it("returns no slots when the agent is boxed in by walls", () => {
    // The outer corner: two neighbours are walls and two are outside the world.
    const corner = { x: 8, y: 14 };
    expect(visitSlots(layout, corner)).toEqual([]);
  });
});

describe("nearestAgent", () => {
  const agents = [
    { id: "far", x: 100, y: 100 },
    { id: "near", x: 112, y: 100 },
    { id: "mid", x: 120, y: 100 },
  ];

  it("picks the closest agent inside the radius", () => {
    expect(nearestAgent(agents, { x: 110, y: 100 }, 20)?.id).toBe("near");
  });

  it("returns null when nothing is close enough", () => {
    expect(nearestAgent(agents, { x: 0, y: 0 }, 20)).toBeNull();
  });

  it("treats the radius as inclusive", () => {
    expect(nearestAgent(agents, { x: 80, y: 100 }, 20)?.id).toBe("far");
  });
});

describe("nearestVisitSlot", () => {
  it("picks a slot the visitor can actually walk to", () => {
    const desk = layout.agents[0]!;
    const from = layout.spawns[0]!;
    const slot = nearestVisitSlot(layout, desk, from);

    expect(slot).not.toBeNull();
    const { tx, ty } = tileAtWorld(slot!.x, slot!.y);
    expect(isWalkableTile(layout, tx, ty)).toBe(true);
    expect(Math.hypot(slot!.x - desk.x, slot!.y - desk.y)).toBeLessThan(48);
  });

  it("returns null when no side of the agent is walkable", () => {
    const corner = { x: 8, y: 14 };
    expect(nearestVisitSlot(layout, corner, layout.spawns[0]!)).toBeNull();
  });
});
