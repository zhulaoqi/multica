// @vitest-environment node

import { describe, expect, it } from "vitest";
import { teamUserIdsFor } from "./team-scope";

const tree = {
  units: [
    { id: "u-rd", can_manage: true },
    { id: "u-platform", can_manage: true },
    { id: "u-sales", can_manage: false },
  ],
  members: [
    { user_id: "me", unit_id: "u-rd" },
    { user_id: "sub", unit_id: "u-platform" },
    { user_id: "peer", unit_id: "u-sales" },
    { user_id: "detached", unit_id: "" },
  ],
};

describe("teamUserIdsFor", () => {
  it("collects the lead and everyone in the units they manage", () => {
    const ids = teamUserIdsFor(tree, "me", false);

    expect(ids).not.toBeNull();
    expect(Array.from(ids!).sort()).toEqual(["me", "sub"]);
  });

  it("returns null for a member who leads nothing", () => {
    const ids = teamUserIdsFor(
      { ...tree, units: tree.units.map((unit) => ({ ...unit, can_manage: false })) },
      "me",
      false,
    );

    expect(ids).toBeNull();
  });

  it("returns null for workspace admins, whose all-agents scope covers it", () => {
    expect(teamUserIdsFor(tree, "me", true)).toBeNull();
  });

  it("returns null without an org tree or a user", () => {
    expect(teamUserIdsFor(null, "me", false)).toBeNull();
    expect(teamUserIdsFor(tree, null, false)).toBeNull();
  });
});
