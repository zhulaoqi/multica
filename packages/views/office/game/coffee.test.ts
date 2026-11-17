// @vitest-environment node

import { describe, expect, it } from "vitest";
import { COFFEE_PROMPTS, pickCoffeePrompt } from "./coffee";

describe("pickCoffeePrompt", () => {
  it("is stable for the same seed", () => {
    expect(pickCoffeePrompt("alex-1")).toBe(pickCoffeePrompt("alex-1"));
    expect(pickCoffeePrompt(42)).toBe(pickCoffeePrompt(42));
  });

  it("always returns a listed prompt", () => {
    for (let index = 0; index < 50; index++) {
      expect(COFFEE_PROMPTS).toContain(pickCoffeePrompt(`seed-${index}`));
    }
  });

  it("reaches more than one prompt across seeds", () => {
    const seen = new Set(
      Array.from({ length: 30 }, (_, index) => pickCoffeePrompt(`seed-${index}`)),
    );
    expect(seen.size).toBeGreaterThan(1);
  });
});
