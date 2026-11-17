// @vitest-environment node

import { describe, expect, it } from "vitest";
import { seasonFor, seasonTheme } from "./season";

function at(month: number) {
  return new Date(2026, month, 15);
}

describe("seasonFor", () => {
  it("maps each quarter to a season", () => {
    expect(seasonFor(at(2)).id).toBe("spring");
    expect(seasonFor(at(4)).id).toBe("spring");
    expect(seasonFor(at(5)).id).toBe("summer");
    expect(seasonFor(at(7)).id).toBe("summer");
    expect(seasonFor(at(8)).id).toBe("autumn");
    expect(seasonFor(at(10)).id).toBe("autumn");
    expect(seasonFor(at(11)).id).toBe("winter");
    expect(seasonFor(at(1)).id).toBe("winter");
  });

  it("gives every season a drifting particle and accent", () => {
    for (const month of [1, 4, 7, 10]) {
      const theme = seasonFor(at(month));
      expect(theme.particle).toBeTruthy();
      expect(theme.accent).toMatch(/^#[0-9a-f]{6}$/);
      expect(theme.tint).toBeGreaterThanOrEqual(0);
    }
  });

  it("returns the same theme object for an id", () => {
    expect(seasonTheme("winter")).toBe(seasonFor(at(0)));
  });
});
