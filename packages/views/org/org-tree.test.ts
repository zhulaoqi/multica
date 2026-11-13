// @vitest-environment node

import { describe, expect, it } from "vitest";
import { agentAvailability, buildOrgForest, squadAvailability, unitPaths } from "./org-tree";

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
        { id: "ag1", name: "Mika", unit_id: "b", can: "", cannot: "", status: "idle" },
        { id: "ag2", name: "Loose", unit_id: "", can: "", cannot: "", status: "offline" },
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

describe("squads in the forest", () => {
  it("groups squads under their unit and reports the ones without one", () => {
    const forest = buildOrgForest({
      units: UNITS,
      agents: [],
      members: [],
      squads: [
        { id: "s1", name: "支付小队", unit_id: "b", leader_id: "ag1", agent_count: 2, working_agents: 0 },
        { id: "s2", name: "流浪小队", unit_id: "", leader_id: "ag2", agent_count: 1, working_agents: 0 },
        { id: "s3", name: "失联小队", unit_id: "gone", leader_id: "ag3", agent_count: 1, working_agents: 0 },
      ],
    });
    expect(forest.roots[0]!.children[0]!.squads.map((squad) => squad.id)).toEqual(["s1"]);
    expect(forest.unassignedSquads.map((squad) => squad.id)).toEqual(["s2", "s3"]);
  });
});

describe("availability", () => {
  it("tells waiting squads apart from busy and empty ones", () => {
    expect(squadAvailability({ agent_count: 2, working_agents: 0 })).toBe("waiting");
    expect(squadAvailability({ agent_count: 2, working_agents: 1 })).toBe("working");
    expect(squadAvailability({ agent_count: 0, working_agents: 0 })).toBe("empty");
  });

  it("maps runtime status to the org view buckets", () => {
    expect(agentAvailability("idle")).toBe("waiting");
    expect(agentAvailability("working")).toBe("working");
    expect(agentAvailability("offline")).toBe("offline");
    expect(agentAvailability("blocked")).toBe("attention");
    expect(agentAvailability("error")).toBe("attention");
    expect(agentAvailability("something-new")).toBe("unknown");
  });
});
