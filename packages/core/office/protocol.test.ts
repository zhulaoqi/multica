// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  parseOfficeError,
  parseOfficePeer,
  parseOfficePeerLeave,
  parseOfficeState,
} from "./protocol";
import { clampToWorld } from "./types";

describe("office protocol", () => {
  it("normalises a peer payload", () => {
    expect(parseOfficePeer({ user_id: "u1", x: 12, y: 34, dir: "left" })).toEqual({
      userId: "u1",
      x: 12,
      y: 34,
      dir: "left",
    });
    // The server uses "" for "facing unknown".
    expect(parseOfficePeer({ user_id: "u1", x: 0, y: 0, dir: "" })?.dir).toBeNull();
    expect(parseOfficePeer({ user_id: "u1", x: 0, y: 0, dir: "diagonal" })?.dir).toBeNull();
  });

  it("rejects peers without usable coordinates", () => {
    expect(parseOfficePeer(null)).toBeNull();
    expect(parseOfficePeer({ user_id: "", x: 1, y: 2 })).toBeNull();
    expect(parseOfficePeer({ user_id: "u1", x: "1", y: 2 })).toBeNull();
    expect(parseOfficePeer({ user_id: "u1", y: 2 })).toBeNull();
  });

  it("keeps valid members of a snapshot and drops the rest", () => {
    const snapshot = parseOfficeState({
      members: [
        { user_id: "u1", x: 1, y: 2, dir: "up" },
        { user_id: "u2", x: "bad", y: 2 },
      ],
    });
    expect(snapshot.members).toEqual([{ userId: "u1", x: 1, y: 2, dir: "up" }]);
  });

  it("falls back to an empty room for a malformed snapshot", () => {
    expect(parseOfficeState({ members: "nope" }).members).toEqual([]);
    expect(parseOfficeState(undefined).members).toEqual([]);
  });

  it("parses leave and error frames", () => {
    expect(parseOfficePeerLeave({ user_id: "u1" })).toBe("u1");
    expect(parseOfficePeerLeave({})).toBeNull();
    expect(parseOfficeError({ error: "invalid_position" })).toBe("invalid_position");
    expect(parseOfficeError(null)).toBeNull();
  });
});

describe("clampToWorld", () => {
  it("clamps into the world bounds", () => {
    expect(clampToWorld(-5, 400)).toEqual({ x: 0, y: 288 });
    expect(clampToWorld(999, 12)).toEqual({ x: 480, y: 12 });
  });

  it("treats non-finite input as the origin", () => {
    expect(clampToWorld(Number.NaN, Number.POSITIVE_INFINITY)).toEqual({ x: 0, y: 0 });
  });
});
