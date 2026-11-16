/**
 * Deterministic pseudo-randomness for the office behaviours.
 *
 * Routines are driven by timers and coin flips; routing them through this
 * interface keeps the scene reproducible in tests and keeps tuning constant
 * free of `Math.random`.
 */
export interface Rng {
  /** Uniform in [0, 1). */
  float(): number;
  /** Uniform integer in [min, max]. */
  int(min: number, max: number): number;
  chance(probability: number): boolean;
  pick<T>(items: readonly T[]): T | null;
  shuffle<T>(items: readonly T[]): T[];
}

/** mulberry32, seeded from a string so an agent's habits stay stable. */
export function createRng(seed: string | number): Rng {
  let state =
    typeof seed === "number" ? seed >>> 0 : hashString(seed);

  const float = () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };

  return {
    float,
    int(min, max) {
      if (max <= min) return min;
      return min + Math.floor(float() * (max - min + 1));
    },
    chance(probability) {
      return float() < probability;
    },
    pick(items) {
      if (items.length === 0) return null;
      return items[Math.floor(float() * items.length)] ?? null;
    },
    shuffle(items) {
      const copy = [...items];
      for (let index = copy.length - 1; index > 0; index--) {
        const swap = Math.floor(float() * (index + 1));
        [copy[index], copy[swap]] = [copy[swap]!, copy[index]!];
      }
      return copy;
    },
  };
}

function hashString(value: string): number {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index++) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}
