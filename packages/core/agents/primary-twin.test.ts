// @vitest-environment node
import { describe, expect, it } from "vitest";

import { findPrimaryTwin, hasPrimaryTwin, ownedTwins } from "./primary-twin";

const agent = (id: string, owner_id: string | null, is_primary?: boolean) => ({ id, owner_id, is_primary });

describe("primary twin selection", () => {
  it("prefers the agent marked primary", () => {
    const agents = [agent("first", "u1"), agent("second", "u1", true), agent("other", "u2", true)];
    expect(findPrimaryTwin(agents, "u1")?.id).toBe("second");
    expect(ownedTwins(agents, "u1").map((item) => item.id)).toEqual(["second", "first"]);
  });

  it("falls back to the first owned agent when an older server omits the flag", () => {
    const agents = [agent("first", "u1"), agent("second", "u1")];
    expect(findPrimaryTwin(agents, "u1")?.id).toBe("first");
    expect(hasPrimaryTwin(agents, "u1")).toBe(true);
  });

  it("returns nothing for an unbound or unknown member", () => {
    const agents = [agent("first", "u1", true), agent("shared", null)];
    expect(findPrimaryTwin(agents, "u2")).toBeNull();
    expect(findPrimaryTwin(agents, null)).toBeNull();
    expect(hasPrimaryTwin(agents, "")).toBe(false);
  });
});
