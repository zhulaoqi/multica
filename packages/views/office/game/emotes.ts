/**
 * The player's emote wheel. Emotes are pure presentation; they never notify
 * the server or interrupt an agent's routine.
 */

export type PlayerEmoteId = "wave" | "check" | "alert" | "coffee";

export interface PlayerEmote {
  id: PlayerEmoteId;
  /** Keyboard shortcut, shown on the wheel. */
  key: string;
}

export const PLAYER_EMOTES: readonly PlayerEmote[] = [
  { id: "wave", key: "1" },
  { id: "check", key: "2" },
  { id: "alert", key: "3" },
  { id: "coffee", key: "4" },
];

export function playerEmoteForKey(key: string): PlayerEmote | null {
  return PLAYER_EMOTES.find((emote) => emote.key === key) ?? null;
}
