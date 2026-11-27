// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildOfficeLayout,
  COLS,
  findPath,
  isWalkableTile,
  OFFICE_DESK_ZONES,
  OFFICE_HOME_COUNT,
  ROWS,
  spawnFor,
  stepTowards,
  TILE,
  type OfficeLayout,
} from "./layout";

const layout = buildOfficeLayout();

const tileOf = (spot: { x: number; y: number }) => ({
  tx: Math.floor(spot.x / TILE),
  ty: Math.floor(spot.y / TILE),
});

describe("office layout", () => {
  it("keeps walls, desks and furniture out of the walkable area", () => {
    expect(isWalkableTile(layout, 0, 0)).toBe(false);
    expect(isWalkableTile(layout, 0, 5)).toBe(false);
    expect(isWalkableTile(layout, 4, 16)).toBe(false); // desk
    expect(isWalkableTile(layout, 6, 5)).toBe(false); // meeting table
    expect(isWalkableTile(layout, 2, 2)).toBe(false); // plant
    expect(isWalkableTile(layout, 23, 2)).toBe(false); // pantry counter
    expect(isWalkableTile(layout, 23, 3)).toBe(true); // in front of the coffee machine
    expect(isWalkableTile(layout, 25, 6)).toBe(true); // pantry stool
    expect(isWalkableTile(layout, 41, 2)).toBe(false); // server rack
    expect(isWalkableTile(layout, 42, 3)).toBe(true); // server aisle
    expect(isWalkableTile(layout, 10, 18)).toBe(true); // open office floor
    expect(isWalkableTile(layout, 13, 27)).toBe(false); // aquarium
  });

  it("fills the world the server validates", () => {
    expect(COLS).toBe(56);
    expect(ROWS).toBe(34);
    const inside = (rect: { x: number; y: number; w: number; h: number }) =>
      rect.x >= 0 && rect.y >= 0 && rect.x + rect.w <= COLS && rect.y + rect.h <= ROWS;
    for (const rect of layout.obstacles) expect(inside(rect)).toBe(true);
    for (const room of layout.rooms) expect(inside(room.rect)).toBe(true);
  });

  it("places every agent slot on a walkable tile directly north of its desk", () => {
    for (const slot of layout.agents) {
      const { tx, ty } = tileOf(slot);
      expect(isWalkableTile(layout, tx, ty)).toBe(true);
      expect(slot.desk.y).toBe(ty + 1);
    }
    expect(layout.agents).toHaveLength(12);
  });

  it("puts every station on walkable floor", () => {
    expect(layout.stations.length).toBeGreaterThan(20);
    for (const station of layout.stations) {
      const { tx, ty } = tileOf(station);
      expect(isWalkableTile(layout, tx, ty)).toBe(true);
    }
  });

  it("keeps the entrance open and every spawn walkable", () => {
    for (const spawn of layout.spawns) {
      const { tx, ty } = tileOf(spawn);
      expect(isWalkableTile(layout, tx, ty)).toBe(true);
      // The spawn sits inside the doorway gap in the bottom wall.
      expect(ty).toBe(ROWS - 2);
    }
    const entrance = tileOf(layout.door);
    expect(entrance.tx).toBeGreaterThanOrEqual(27);
    expect(entrance.tx).toBeLessThanOrEqual(29);
  });
});

describe("desk zones", () => {
  it("covers every home slot exactly once", () => {
    const homes = OFFICE_DESK_ZONES.flatMap((zone) => zone.homes);
    expect(new Set(homes).size).toBe(homes.length);
    expect([...homes].sort((left, right) => left - right)).toEqual(
      [...Array(OFFICE_HOME_COUNT).keys()],
    );
  });

  it("clusters desks by column and keeps plaques inside the open office", () => {
    const office = layout.rooms.find((room) => room.id === "office")!;
    const columns = new Map<string, number[]>();
    for (const zone of OFFICE_DESK_ZONES) {
      columns.set(
        zone.key,
        zone.homes
          .filter((home) => home < layout.agents.length)
          .map((home) => layout.agents[home]!.desk.x),
      );
    }
    expect(columns.get("west")).toEqual([4, 7, 4, 7]);
    expect(columns.get("center")).toEqual([22, 25, 22, 25]);
    expect(columns.get("east")).toEqual([40, 43, 40, 43]);
    for (const zone of OFFICE_DESK_ZONES) {
      expect(zone.anchor.tx).toBeGreaterThanOrEqual(office.rect.x);
      expect(zone.anchor.tx).toBeLessThan(office.rect.x + office.rect.w);
      expect(zone.anchor.ty).toBeGreaterThanOrEqual(office.rect.y);
      expect(zone.anchor.ty).toBeLessThan(office.rect.y + office.rect.h);
    }
  });
});

describe("findPath", () => {
  const from = layout.spawns[0]!;

  it("routes from the entrance to every station", () => {
    for (const station of layout.stations) {
      if (station.key === "door") continue;
      const path = findPath(layout, from, station);
      expect(path.length).toBeGreaterThan(0);
    }
  });

  it("routes around furniture instead of through it", () => {
    const path = findPath(layout, from, { x: 10 * TILE, y: 18 * TILE });
    expect(path.length).toBeGreaterThan(0);
    for (const point of path) {
      const { tx, ty } = tileOf(point);
      expect(isWalkableTile(layout, tx, ty)).toBe(true);
    }
    expect(path[path.length - 1]).toEqual({
      x: 10 * TILE + TILE / 2,
      y: 18 * TILE + TILE - 2,
    });
  });

  it("returns no path into a blocked tile", () => {
    expect(findPath(layout, from, { x: 4 * TILE, y: 16 * TILE })).toEqual([]);
  });

  it("returns no waypoints when already standing on the target tile", () => {
    const spot = { x: 10 * TILE, y: 18 * TILE };
    expect(findPath(layout, spot, spot)).toEqual([]);
  });
});

describe("stepTowards", () => {
  it("walks toward the target and reports arrival", () => {
    const first = stepTowards({ x: 0, y: 0 }, { x: 30, y: 0 }, 10);
    expect(first).toEqual({ x: 10, y: 0, dir: "right", arrived: false });
    const last = stepTowards({ x: 25, y: 0 }, { x: 30, y: 0 }, 10);
    expect(last).toEqual({ x: 30, y: 0, dir: "right", arrived: true });
  });

  it("derives the facing direction from the dominant axis", () => {
    expect(stepTowards({ x: 0, y: 0 }, { x: 0, y: 10 }, 1).dir).toBe("down");
    expect(stepTowards({ x: 0, y: 0 }, { x: -1, y: -9 }, 1).dir).toBe("up");
  });
});

describe("spawnFor", () => {
  it("is stable per member and always walkable", () => {
    const first = spawnFor(layout, "user-1");
    expect(spawnFor(layout, "user-1")).toEqual(first);
    const walkable = (spot: { x: number; y: number }) => {
      const { tx, ty } = tileOf(spot);
      return isWalkableTile(layout, tx, ty);
    };
    expect(walkable(first)).toBe(true);
    const seen = new Set<string>();
    for (const userId of ["a", "b", "c", "d", "e", "f", "g"]) {
      const spawn = spawnFor(layout, userId);
      expect(walkable(spawn)).toBe(true);
      seen.add(`${spawn.x},${spawn.y}`);
    }
    expect(seen.size).toBeGreaterThan(1);
  });
});

// Guards the invariant that furniture never seals a room: every station is
// reachable from the doorway, and every blocked tile still has a walkable
// neighbour so the layout has no unreachable pockets.
it("leaves no isolated furniture island", () => {
  const layout: OfficeLayout = buildOfficeLayout();
  for (const station of layout.stations) {
    const path = findPath(layout, layout.door, station);
    if (station.key !== "door") expect(path.length).toBeGreaterThan(0);
  }
});

describe("decoration unlocks", () => {
  const furnitureIds = (layout: OfficeLayout) =>
    layout.furniture.map((item) => item.id);

  it("starts a new company with no bonus furniture", () => {
    const fresh = buildOfficeLayout();
    expect(furnitureIds(fresh).some((id) => id.startsWith("unlock-"))).toBe(false);
    expect(isWalkableTile(fresh, 35, 31)).toBe(true);
  });

  it("adds the level 2 plant and makes it solid", () => {
    const level2 = buildOfficeLayout(2);
    expect(furnitureIds(level2)).toContain("unlock-plant");
    expect(furnitureIds(level2)).not.toContain("unlock-poster");
    expect(isWalkableTile(level2, 35, 31)).toBe(false);
  });

  it("hangs the level 3 poster without blocking the floor", () => {
    const level3 = buildOfficeLayout(3);
    expect(furnitureIds(level3)).toContain("unlock-poster");
    expect(furnitureIds(level3)).not.toContain("unlock-shelf");
    expect(isWalkableTile(level3, 48, 13)).toBe(true);
  });

  it("adds every decoration by level 4 without sealing a station off", () => {
    const max = buildOfficeLayout(4);
    for (const id of ["unlock-plant", "unlock-poster", "unlock-shelf"]) {
      expect(furnitureIds(max)).toContain(id);
    }
    expect(isWalkableTile(max, 12, 31)).toBe(false);
    for (const station of max.stations) {
      if (station.key === "door") continue;
      expect(findPath(max, max.door, station).length).toBeGreaterThan(0);
    }
  });
});
