// @vitest-environment node
import { describe, expect, it } from "vitest";
import { docsHref, docsLocalePrefix } from "./docs-locale";

describe("docsLocalePrefix", () => {
  it.each([
    ["en", ""],
    [undefined, ""],
    ["zh-Hans", "/zh"],
    ["zh-Hant", "/zh-Hant"],
    ["ja", "/ja"],
    ["ko", "/ko"],
    ["fr", "/fr"],
  ])("maps %s to %j", (language, expected) => {
    expect(docsLocalePrefix(language)).toBe(expected);
  });
});

describe("docsHref", () => {
  // Same-origin only: the fork ships its own docs under /docs, so no link may
  // escape to the public multica.ai site.
  it.each([
    ["en", "/agents", "/docs/agents"],
    ["zh-Hans", "/agents", "/docs/zh/agents"],
    ["zh-Hant", "/autopilots#event-filters", "/docs/zh-Hant/autopilots#event-filters"],
    [undefined, "", "/docs"],
  ])("builds %s links from %j", (language, path, expected) => {
    expect(docsHref(language, path)).toBe(expected);
  });
});
