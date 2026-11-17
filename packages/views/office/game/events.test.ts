// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  OFFICE_EVENTS,
  laterEventDelay,
  nextEventDelay,
  pickOfficeEvent,
} from "./events";

describe("office random events", () => {
  it("arrives after a visible pause", () => {
    expect(nextEventDelay(() => 0)).toBe(45_000);
    expect(nextEventDelay(() => 1)).toBe(110_000);
    expect(laterEventDelay(() => 0)).toBe(55_000);
    expect(laterEventDelay(() => 1)).toBe(120_000);
  });

  it("picks an event for every random value, including the edges", () => {
    expect(pickOfficeEvent(() => 0)).toBe(OFFICE_EVENTS[0]);
    expect(pickOfficeEvent(() => 1)).toBe(OFFICE_EVENTS[OFFICE_EVENTS.length - 1]);
    for (let index = 0; index < 20; index++) {
      expect(OFFICE_EVENTS).toContain(pickOfficeEvent(() => index / 20));
    }
  });
});
