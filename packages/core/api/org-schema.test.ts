// @vitest-environment node
import { describe, expect, it } from "vitest";
import { EMPTY_ORG_TREE, OrgTreeSchema } from "./org-schema";
import { parseWithFallback } from "./schema";

describe("org tree wire compatibility", () => {
  it("defaults squads and the caller's unit when an older server omits them", () => {
    const parsed = OrgTreeSchema.parse({
      units: [{ id: "dept-1", name: "研发中心" }],
      agents: [{ id: "agent-1", name: "Reviewer" }],
      members: [{ user_id: "u-1", name: "Ada" }],
      can_manage: false,
    });

    expect(parsed.squads).toEqual([]);
    expect(parsed.my_unit_id).toBe("");
    expect(parsed.agents[0]?.status).toBe("");
  });

  it("keeps squad scheduling counts and agent availability", () => {
    const parsed = OrgTreeSchema.parse({
      units: [],
      agents: [{ id: "agent-1", name: "Reviewer", unit_id: "dept-1", status: "idle" }],
      members: [],
      squads: [{
        id: "squad-1",
        name: "支付小队",
        unit_id: "dept-1",
        leader_id: "agent-1",
        agent_count: 2,
        working_agents: 0,
      }],
      my_unit_id: "dept-1",
    });

    expect(parsed.agents[0]?.status).toBe("idle");
    expect(parsed.squads[0]).toMatchObject({ agent_count: 2, working_agents: 0, unit_id: "dept-1" });
    expect(parsed.my_unit_id).toBe("dept-1");
  });

  it("falls back to an empty tree on malformed payloads", () => {
    const parsed = parseWithFallback(
      { units: "not-an-array", agents: [{ name: 5 }] },
      OrgTreeSchema,
      EMPTY_ORG_TREE,
      { endpoint: "GET /api/org/units" },
    );

    expect(parsed).toEqual(EMPTY_ORG_TREE);
  });
});
