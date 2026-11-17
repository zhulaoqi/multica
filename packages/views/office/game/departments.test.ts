// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  DEPARTMENT_COMPLETIONS_PER_LEVEL,
  MAX_DEPARTMENT_LEVEL,
  departmentRooms,
} from "./departments";

const units = [
  { id: "u-eng", name: "Engineering" },
  { id: "u-design", name: "Design" },
  { id: "u-empty", name: "Empty" },
];

const agents = [
  { id: "a-1", unit_id: "u-eng" },
  { id: "a-2", unit_id: "u-eng" },
  { id: "a-3", unit_id: "u-design" },
  { id: "a-4", unit_id: "" },
  { id: "a-5", unit_id: "u-missing" },
];

describe("departmentRooms", () => {
  it("ranks staffed departments by completed work", () => {
    const completed = new Map([
      ["a-1", 12],
      ["a-2", 8],
      ["a-3", 50],
      ["a-4", 100],
      ["a-5", 100],
    ]);
    const rooms = departmentRooms(units, agents, completed);
    expect(rooms.map((room) => room.name)).toEqual(["Design", "Engineering"]);
    expect(rooms[0]?.completed).toBe(50);
    expect(rooms[1]?.completed).toBe(20);
  });

  it("skips unassigned agents and unknown units", () => {
    const rooms = departmentRooms(units, agents, new Map());
    expect(rooms.map((room) => room.unitId)).toEqual(["u-design", "u-eng"]);
  });

  it("turns completions into capped levels and stars", () => {
    const completed = new Map([["a-1", DEPARTMENT_COMPLETIONS_PER_LEVEL * 4]]);
    const rooms = departmentRooms(units, agents, completed, ["office"]);
    expect(rooms[0]).toMatchObject({ room: "office", level: 5, stars: 5 });

    const huge = new Map([["a-1", 10_000]]);
    const capped = departmentRooms(units, agents, huge, ["office"]);
    expect(capped[0]?.level).toBe(MAX_DEPARTMENT_LEVEL);
  });

  it("never assigns more departments than rooms", () => {
    const rooms = departmentRooms(units, agents, new Map(), ["office", "meeting"]);
    expect(rooms).toHaveLength(2);
    expect(rooms.map((room) => room.room)).toEqual(["office", "meeting"]);
  });

  it("ties break on name so rooms stay stable", () => {
    const rooms = departmentRooms(
      [...units].reverse(),
      [...agents].reverse(),
      new Map(),
    );
    expect(rooms.map((room) => room.name)).toEqual(["Design", "Engineering"]);
  });
});
