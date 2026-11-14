// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildOfficeLayout,
  findPath,
  isWalkableTile,
  spawnFor,
  stepTowards,
  TILE,
  type OfficeLayout,
} from "./layout";

const layout = buildOfficeLayout();

describe("office layout", () => {
  it("keeps desks, furniture and walls out of the walkable area", () => {
    expect(isWalkableTile(layout, 0, 0)).toBe(false);
    expect(isWalkableTile(layout, 0, 5)).toBe(false);
    expect(isWalkableTile(layout, 12, 4)).toBe(false); // desk
    expect(isWalkableTile(layout, 4, 9)).toBe(false); // meeting table
    expect(isWalkableTile(layout, 2, 2)).toBe(false); // plant
    expect(isWalkableTile(layout, 14, 7)).toBe(true);
  });

  it("places every agent slot on a walkable tile in front of a desk", () => {
    for (const slot of layout.agents) {
      const tx = Math.floor(slot.x / TILE);
      const ty = Math.floor(slot.y / TILE);
      expect(isWalkableTile(layout, tx, ty)).toBe(true);
      expect(slot.desk.y).toBe(ty + 1);
    }
  });

  it("keeps the entrance open and every spawn walkable", () => {
    for (const spawn of layout.spawns) {
      expect(isWalkableTile(layout, Math.floor(spawn.x / TILE), Math.floor(spawn.y / TILE))).toBe(true);
    }
  });
});

describe("findPath", () => {
  const openSpot = { x: 14 * TILE, y: 7 * TILE };

  it("routes around furniture instead of through it", () => {
    const path = findPath(layout, layout.spawns[0]!, openSpot);
    expect(path.length).toBeGreaterThan(0);
    for (const point of path) {
      expect(
        isWalkableTile(layout, Math.floor(point.x / TILE), Math.floor(point.y / TILE)),
      ).toBe(true);
    }
    expect(path[path.length - 1]).toEqual({
      x: 14 * TILE + TILE / 2,
      y: 7 * TILE + TILE - 2,
    });
  });

  it("returns no path into a blocked tile", () => {
    expect(
      findPath(layout, layout.spawns[0]!, { x: 12 * TILE, y: 4 * TILE }),
    ).toEqual([]);
  });

  it("returns no waypoints when already standing on the target tile", () => {
    expect(findPath(layout, openSpot, openSpot)).toEqual([]);
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
    const tile = (spot: { x: number; y: number }) =>
      isWalkableTile(layout, Math.floor(spot.x / TILE), Math.floor(spot.y / TILE));
    expect(tile(first)).toBe(true);
    const seen = new Set<string>();
    for (const userId of ["a", "b", "c", "d", "e", "f", "g"]) {
      const spawn = spawnFor(layout, userId);
      expect(tile(spawn)).toBe(true);
      seen.add(`${spawn.x},${spawn.y}`);
    }
    expect(seen.size).toBeGreaterThan(1);
  });
});

// Guards the invariant that the layout fills the world the server validates
// against: a desk placed outside 30 x 18 would be unreachable and unsendable.
it("keeps all furniture inside the world", () => {
  const inside = (rect: { x: number; y: number; w: number; h: number }) =>
    rect.x >= 0 && rect.y >= 0 && rect.x + rect.w <= 30 && rect.y + rect.h <= 18;
  const layout: OfficeLayout = buildOfficeLayout();
  for (const rect of layout.obstacles) expect(inside(rect)).toBe(true);
});
