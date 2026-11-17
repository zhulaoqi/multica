/**
 * Kitchen small talk. Each prompt has a stable id so all seven locales can
 * translate it and a refresh does not need to read a translated string back.
 */

export type CoffeePromptId = "shipped" | "blocked" | "learned" | "next" | "thanks";

export const COFFEE_PROMPTS: readonly CoffeePromptId[] = [
  "shipped",
  "blocked",
  "learned",
  "next",
  "thanks",
];

function hash(value: string): number {
  let result = 0;
  for (let index = 0; index < value.length; index++) {
    result = (result * 31 + value.charCodeAt(index)) % 100_003;
  }
  return result;
}

/** A stable prompt per seed; callers change the seed to reroll. */
export function pickCoffeePrompt(seed: string | number): CoffeePromptId {
  const index = hash(String(seed)) % COFFEE_PROMPTS.length;
  return COFFEE_PROMPTS[index]!;
}
