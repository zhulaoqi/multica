// @vitest-environment node
import { describe, expect, it } from "vitest";
import { OFFICE_HOME_COUNT } from "../scene/layout";
import { departmentLevel } from "./departments";
import { assignDeskZones, type ZoneAgent } from "./zones";

const units = [
  { id: "aihub", name: "AIHUB", position: 0 },
  { id: "bi", name: "BI中台", position: 1 },
];

const agentsOf = (...ids: string[]): ZoneAgent[] => ids.map((id) => ({ id }));

const homeOf = (seats: { id: string; home: number }[]) =>
  Object.fromEntries(seats.map((seat) => [seat.id, seat.home]));

describe("assignDeskZones", () => {
  it("gives each department its own desk cluster", () => {
    const agents = agentsOf("a1", "a2", "a3", "b1", "b2");
    const unitByAgent = new Map([
      ["a1", "aihub"],
      ["a2", "aihub"],
      ["a3", "aihub"],
      ["b1", "bi"],
      ["b2", "bi"],
    ]);
    const { seats, zones, unseated } = assignDeskZones(
      agents,
      unitByAgent,
      units,
      new Map(),
    );

    expect(homeOf(seats)).toEqual({
      a1: 0,
      a2: 1,
      a3: 6,
      b1: 2,
      b2: 3,
    });
    expect(zones.map((zone) => [zone.key, zone.label])).toEqual([
      ["west", "AIHUB"],
      ["center", "BI中台"],
    ]);
    expect(unseated).toEqual([]);
  });

  it("spills a large department into the next cluster", () => {
    const ids = Array.from({ length: 10 }, (_, index) => `agent-${index}`);
    const unitByAgent = new Map(ids.map((id) => [id, "aihub"]));
    const { seats, zones } = assignDeskZones(
      agentsOf(...ids),
      unitByAgent,
      [units[0]!],
      new Map(),
    );

    expect(seats.map((seat) => seat.home)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 18, 19]);
    expect(zones.map((zone) => [zone.key, zone.label])).toEqual([
      ["west", "AIHUB"],
      ["center", "AIHUB"],
      ["east", "AIHUB"],
    ]);
  });

  it("caps the floor at the home slots and reports who did not fit", () => {
    const ids = Array.from({ length: 25 }, (_, index) => `agent-${index}`);
    const unitByAgent = new Map(ids.map((id) => [id, "aihub"]));
    const { seats, zones, unseated } = assignDeskZones(
      agentsOf(...ids),
      unitByAgent,
      [units[0]!],
      new Map(),
    );

    expect(seats).toHaveLength(OFFICE_HOME_COUNT);
    expect(new Set(seats.map((seat) => seat.home)).size).toBe(OFFICE_HOME_COUNT);
    expect(unseated).toHaveLength(5);
    // The extra standing spots down the right wall are used before anyone is
    // turned away.
    expect(seats.some((seat) => seat.home >= 12)).toBe(true);
    expect(zones).toHaveLength(3);
  });

  it("seats agents without a visible department last and gives them no plaque", () => {
    const agents = agentsOf("known-1", "known-2", "loose-1", "loose-2");
    const unitByAgent = new Map([
      ["known-1", "aihub"],
      ["known-2", "aihub"],
      ["loose-1", "unknown-unit"],
    ]);
    const { seats, zones, unseated } = assignDeskZones(
      agents,
      unitByAgent,
      units,
      new Map(),
    );

    expect(homeOf(seats)).toEqual({
      "known-1": 0,
      "known-2": 1,
      "loose-1": 2,
      "loose-2": 3,
    });
    expect(zones.map((zone) => zone.label)).toEqual(["AIHUB"]);
    expect(unseated).toEqual([]);
  });

  it("keeps the historical input order when no department is known", () => {
    const agents = agentsOf("a", "b", "c");
    const { seats, zones } = assignDeskZones(agents, new Map(), units, new Map());

    expect(homeOf(seats)).toEqual({ a: 0, b: 1, c: 2 });
    expect(zones).toEqual([]);
  });

  it("is stable when agents and units arrive in a different order", () => {
    const agents = agentsOf("a1", "a2", "b1", "b2", "b3");
    const unitByAgent = new Map([
      ["a1", "aihub"],
      ["a2", "aihub"],
      ["b1", "bi"],
      ["b2", "bi"],
      ["b3", "bi"],
    ]);
    const first = assignDeskZones(agents, unitByAgent, units, new Map());
    const reversed = assignDeskZones(
      [...agents].reverse(),
      unitByAgent,
      [...units].reverse(),
      new Map(),
    );

    expect(homeOf(reversed.seats)).toEqual(homeOf(first.seats));
    expect(reversed.zones.map((zone) => zone.label)).toEqual(
      first.zones.map((zone) => zone.label),
    );
  });

  it("places departments beyond the three clusters without labels", () => {
    const allUnits = Array.from({ length: 4 }, (_, index) => ({
      id: `unit-${index}`,
      name: `Unit ${index}`,
      position: index,
    }));
    const ids = Array.from({ length: 12 }, (_, index) => `agent-${index}`);
    const unitByAgent = new Map(
      ids.map((id, index) => [id, `unit-${Math.floor(index / 3)}`]),
    );
    const { seats, zones, unseated } = assignDeskZones(
      agentsOf(...ids),
      unitByAgent,
      allUnits,
      new Map(),
    );

    expect(seats).toHaveLength(12);
    expect(new Set(seats.map((seat) => seat.home)).size).toBe(12);
    expect(unseated).toEqual([]);
    expect(zones.map((zone) => zone.label)).toEqual([
      "Unit 0",
      "Unit 1",
      "Unit 2",
    ]);
  });

  it("reads plaque levels from completed work without moving seats", () => {
    const agents = agentsOf("a1", "a2", "b1");
    const unitByAgent = new Map([
      ["a1", "aihub"],
      ["a2", "aihub"],
      ["b1", "bi"],
    ]);
    const before = assignDeskZones(agents, unitByAgent, units, new Map());
    const after = assignDeskZones(
      agents,
      unitByAgent,
      units,
      new Map([
        ["a1", 15],
        ["a2", 20],
        ["b1", 200],
      ]),
    );

    expect(homeOf(after.seats)).toEqual(homeOf(before.seats));
    expect(after.zones.find((zone) => zone.label === "AIHUB")?.level).toBe(4);
    expect(after.zones.find((zone) => zone.label === "BI中台")?.level).toBe(
      departmentLevel(200),
    );
    expect(departmentLevel(200)).toBe(9);
  });
});
