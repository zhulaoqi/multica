/**
 * The office dresses itself for the season. The theme only changes colour and
 * drifting particles; it never hides furniture or changes routing.
 */

export type SeasonId = "spring" | "summer" | "autumn" | "winter";

export type SeasonParticle = "petal" | "firefly" | "leaf" | "snow";

export interface SeasonTheme {
  id: SeasonId;
  /** Ambient particle that drifts across the room. */
  particle: SeasonParticle;
  /** Accent used by the season chip and the minimap border. */
  accent: string;
  /** Particle tint in 0xRRGGBB form, matching the scene palette. */
  tint: number;
}

const THEMES: Record<SeasonId, SeasonTheme> = {
  spring: { id: "spring", particle: "petal", accent: "#e58fb4", tint: 0xf3b7cf },
  summer: { id: "summer", particle: "firefly", accent: "#d9a34f", tint: 0xf5d98a },
  autumn: { id: "autumn", particle: "leaf", accent: "#cf7a43", tint: 0xd9975a },
  winter: { id: "winter", particle: "snow", accent: "#7fb4d9", tint: 0xe8f4fb },
};

/**
 * Southern-hemisphere offices read the same calendar differently, but the
 * scene has no workspace locale at this layer; northern seasons are the
 * documented default.
 */
export function seasonFor(date: Date): SeasonTheme {
  const month = date.getMonth();
  if (month >= 2 && month <= 4) return THEMES.spring;
  if (month >= 5 && month <= 7) return THEMES.summer;
  if (month >= 8 && month <= 10) return THEMES.autumn;
  return THEMES.winter;
}

export function seasonTheme(id: SeasonId): SeasonTheme {
  return THEMES[id];
}
