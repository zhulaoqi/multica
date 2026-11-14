import { describe, expect, it } from "vitest";
import { buildOfficeLayout, isWalkableWorld } from "./layout";
import {
  assignStations,
  deskStation,
  idleErrandAt,
  leisureStations,
  meetingStations,
} from "./stations";

const layout = buildOfficeLayout();

describe("office stations", () => {
  it("seats every meeting agent on walkable floor", () => {
    const seats = meetingStations(layout);
    expect(seats).toHaveLength(4);
    for (const seat of seats) {
      expect(isWalkableWorld(layout, seat.x, seat.y)).toBe(true);
      expect(seat.seat).toBe(true);
    }
  });

  it("points meeting seats at the table", () => {
    const seats = meetingStations(layout);
    const tableTop = layout.meetingTable.y;
    for (const seat of seats) {
      const row = Math.floor(seat.y / 16);
      expect(seat.dir).toBe(row < tableTop ? "down" : "up");
    }
  });

  it("places every leisure spot on walkable floor, out of the desks", () => {
    const spots = leisureStations(layout);
    expect(spots.length).toBeGreaterThanOrEqual(4);
    const keys = new Set(spots.map((spot) => spot.key));
    expect(keys.size).toBe(spots.length);
    for (const spot of spots) {
      expect(isWalkableWorld(layout, spot.x, spot.y)).toBe(true);
      expect(spot.kind).toBe("leisure");
      expect(spot.seat).toBe(false);
    }
  });

  it("keeps one desk per agent slot, seated", () => {
    layout.agents.forEach((slot, index) => {
      const station = deskStation(slot, index);
      expect(station.key).toBe(`desk:${index}`);
      expect(station.seat).toBe(true);
      expect(isWalkableWorld(layout, station.x, station.y)).toBe(true);
    });
  });

  it("spreads agents over seats instead of letting them stack", () => {
    const seats = meetingStations(layout);
    const assigned = seats.map((_, index) => assignStations(seats, index)?.key);
    expect(assigned).toEqual(seats.map((seat) => seat.key));
    expect(assignStations(seats, seats.length)?.key).toBe(seats[0]?.key);
    expect(assignStations([], 0)).toBeNull();
  });

  it("alternates idle agents between desk and their shared spot", () => {
    const home = deskStation(layout.agents[0]!, 0);
    const errand = leisureStations(layout)[1]!;
    expect(idleErrandAt(0, home, errand).key).toBe(home.key);
    expect(idleErrandAt(2, home, errand).key).toBe(home.key);
    expect(idleErrandAt(1, home, errand).key).toBe(errand.key);
    expect(idleErrandAt(3, home, null).key).toBe(home.key);
  });
});
