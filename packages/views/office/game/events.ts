/**
 * Random office events keep the room alive between real work. They are
 * cosmetic and deterministic when a caller injects a random source.
 */

export type OfficeEventId =
  | "cat"
  | "delivery"
  | "coffee"
  | "plane"
  | "phone"
  | "lights";

export const OFFICE_EVENTS: readonly OfficeEventId[] = [
  "cat",
  "delivery",
  "coffee",
  "plane",
  "phone",
  "lights",
];

const FIRST_EVENT_MS = 45_000;
const EVENT_GAP_MIN_MS = 55_000;
const EVENT_GAP_SPREAD_MS = 65_000;

type RandomSource = () => number;

/** Wall-clock delay before the next event; the first one arrives sooner. */
export function nextEventDelay(random: RandomSource = Math.random): number {
  return FIRST_EVENT_MS + random() * EVENT_GAP_SPREAD_MS;
}

/** Once the office has settled, events are spaced out. */
export function laterEventDelay(random: RandomSource = Math.random): number {
  return EVENT_GAP_MIN_MS + random() * EVENT_GAP_SPREAD_MS;
}

export function pickOfficeEvent(random: RandomSource = Math.random): OfficeEventId {
  const index = Math.min(
    OFFICE_EVENTS.length - 1,
    Math.max(0, Math.floor(random() * OFFICE_EVENTS.length)),
  );
  return OFFICE_EVENTS[index]!;
}
