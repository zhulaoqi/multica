// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { OrgSquad } from "@multica/core/api";
import { agentAvailability, buildOrgForest, memberTwin, squadAvailability, unitPaths, unitSubtreeIDs } from "./org-tree";

function squad(id: string, name: string, unitId: string, leaderId: string, agentCount: number): OrgSquad {
  return {
    id,
    name,
    unit_id: unitId,
    leader_id: leaderId,
    agent_count: agentCount,
    working_agents: 0,
    wip_limit: 3,
    max_children: 8,
    active_issues: 0,
    review_issues: 0,
    blocked_issues: 0,
  };
}

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

describe("unitSubtreeIDs", () => {
  it("covers the unit and every department below it, not siblings or parents", () => {
    expect([...unitSubtreeIDs(UNITS, "a")].sort()).toEqual(["a", "b", "c"]);
    expect([...unitSubtreeIDs(UNITS, "b")]).toEqual(["b"]);
    expect([...unitSubtreeIDs(UNITS, "missing")]).toEqual(["missing"]);
  });
});

describe("buildOrgForest", () => {
  it("nests children and collects agents and members without a unit", () => {
    const forest = buildOrgForest({
      units: UNITS,
      agents: [
        { id: "ag1", name: "Mika", unit_id: "b", owner_id: "", runtime_mode: "local", can: "", cannot: "", status: "idle" },
        { id: "ag2", name: "Loose", unit_id: "", owner_id: "", runtime_mode: "local", can: "", cannot: "", status: "offline" },
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
        squad("s1", "支付小队", "b", "ag1", 2),
        squad("s2", "流浪小队", "", "ag2", 1),
        squad("s3", "失联小队", "gone", "ag3", 1),
      ],
    });
    expect(forest.roots[0]!.children[0]!.squads.map((squad) => squad.id)).toEqual(["s1"]);
    expect(forest.unassignedSquads.map((squad) => squad.id)).toEqual(["s2", "s3"]);
  });
});

describe("memberTwin", () => {
  const agents = [
    { id: "ag1", name: "Mika", unit_id: "b", owner_id: "u1", runtime_mode: "local", can: "", cannot: "", status: "idle" },
    { id: "ag2", name: "Loose", unit_id: "", owner_id: "", runtime_mode: "local", can: "", cannot: "", status: "offline" },
  ];

  it("binds an agent to the member that registered it", () => {
    expect(memberTwin(agents, "u1")?.id).toBe("ag1");
  });

  it("returns null for members without a twin or without an id", () => {
    expect(memberTwin(agents, "u2")).toBeNull();
    expect(memberTwin(agents, "")).toBeNull();
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
