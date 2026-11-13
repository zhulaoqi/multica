// @vitest-environment node

import { describe, expect, it } from "vitest";
import { buildOrgForest, unitPaths } from "./org-tree";

const UNITS = [
  { id: "a", parent_id: "", name: "研发中心", lead_user_id: "", can_manage: true, description: "", position: 0 },
  { id: "b", parent_id: "a", name: "支付组", lead_user_id: "", can_manage: true, description: "", position: 0 },
  { id: "c", parent_id: "a", name: "结算组", lead_user_id: "", can_manage: false, description: "", position: 0 },
];

describe("unitPaths", () => {
  it("keeps the root name flat and joins children with a slash", () => {
    const paths = unitPaths(UNITS);
    expect(paths.get("a")).toBe("研发中心");
    expect(paths.get("b")).toBe("研发中心/支付组");
    expect(paths.get("c")).toBe("研发中心/结算组");
  });
});

describe("buildOrgForest", () => {
  it("nests children and collects agents and members without a unit", () => {
    const forest = buildOrgForest({
      units: UNITS,
      agents: [
        { id: "ag1", name: "Mika", unit_id: "b", can: "", cannot: "" },
        { id: "ag2", name: "Loose", unit_id: "", can: "", cannot: "" },
      ],
      members: [
        { user_id: "u1", unit_id: "b", name: "Ann", email: "ann@example.com", avatar_url: null },
        { user_id: "u2", unit_id: "gone", name: "Bob", email: "bob@example.com", avatar_url: null },
      ],
    });
    expect(forest.roots).toHaveLength(1);
    expect(forest.roots[0]!.children.map((node) => node.unit.id)).toEqual(["b", "c"]);
    expect(forest.roots[0]!.children[0]!.agents.map((agent) => agent.id)).toEqual(["ag1"]);
    expect(forest.roots[0]!.children[0]!.members.map((member) => member.user_id)).toEqual(["u1"]);
    expect(forest.unassignedAgents.map((agent) => agent.id)).toEqual(["ag2"]);
    expect(forest.unassignedMembers.map((member) => member.user_id)).toEqual(["u2"]);
  });
});
