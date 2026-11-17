// @vitest-environment node

import { describe, expect, it } from "vitest";
import { PLAYER_EMOTES, playerEmoteForKey } from "./emotes";

describe("player emote wheel", () => {
  it("assigns each emote a unique number key", () => {
    const keys = PLAYER_EMOTES.map((emote) => emote.key);
    expect(new Set(keys).size).toBe(PLAYER_EMOTES.length);
    expect(keys).toEqual(["1", "2", "3", "4"]);
  });

  it("resolves shortcuts and ignores everything else", () => {
    expect(playerEmoteForKey("1")?.id).toBe("wave");
    expect(playerEmoteForKey("4")?.id).toBe("coffee");
    expect(playerEmoteForKey("t")).toBeNull();
    expect(playerEmoteForKey("")).toBeNull();
  });
});
