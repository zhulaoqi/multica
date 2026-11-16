// @vitest-environment node
import { describe, expect, it } from "vitest";
import { pickVisitPair } from "./social";

const at = (id: string, x: number, y = 0) => ({ id, x, y });

describe("pickVisitPair", () => {
  it("returns null when there are fewer than two candidates", () => {
    expect(pickVisitPair([], 200)).toBeNull();
    expect(pickVisitPair([at("a", 0)], 200)).toBeNull();
  });

  it("picks the closest pair within the radius", () => {
    const pair = pickVisitPair(
      [at("a", 0), at("b", 300), at("c", 40), at("d", 330)],
      200,
    );
    expect(pair).toEqual({
      visitor: at("b", 300),
      host: at("d", 330),
    });
  });

  it("ignores agents further apart than the radius", () => {
    expect(pickVisitPair([at("a", 0), at("b", 201)], 200)).toBeNull();
  });

  it("keeps the earlier pair when distances tie", () => {
    const pair = pickVisitPair(
      [at("a", 0), at("b", 100), at("c", 200), at("d", 300)],
      200,
    );
    expect(pair?.visitor.id).toBe("a");
    expect(pair?.host.id).toBe("b");
  });
});
