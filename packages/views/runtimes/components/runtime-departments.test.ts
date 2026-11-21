// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { OrgTree } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types";
import { groupMachinesByDepartment } from "./runtime-departments";
import type { RuntimeMachine } from "./runtime-machines";

function machine(id: string, ownerId: string): RuntimeMachine {
  return {
    id,
    runtimes: [{ id: `rt-${id}`, owner_id: ownerId } as AgentRuntime],
  } as RuntimeMachine;
}

function unit(id: string, name: string, parentId = "") {
  return { id, name, parent_id: parentId, lead_user_id: "", can_manage: false, description: "", position: 0, intake_sources: [] };
}

function member(userId: string, unitId: string) {
  return { user_id: userId, unit_id: unitId, name: userId, email: "", avatar_url: null, in_project: true };
}

const tree: Pick<OrgTree, "units" | "members" | "my_unit_id"> = {
  units: [unit("rd", "研发部"), unit("dev1", "开发一组", "rd"), unit("dev2", "开发二组", "rd")],
  members: [member("lead", "rd"), member("kinch", "dev2"), member("phil", "dev1")],
  my_unit_id: "rd",
};

describe("groupMachinesByDepartment", () => {
  it("buckets by owner department with the viewer's own first and unassigned last", () => {
    const groups = groupMachinesByDepartment(
      [machine("a", "kinch"), machine("b", "stranger"), machine("c", "lead"), machine("d", "phil")],
      tree,
      "lead",
    );
    expect(groups?.map((group) => [group.path, group.machines.map((m) => m.id)])).toEqual([
      ["研发部", ["c"]],
      ["研发部/开发一组", ["d"]],
      ["研发部/开发二组", ["a"]],
      ["", ["b"]],
    ]);
  });

  it("bands a lead's team machines even when they share one department", () => {
    const groups = groupMachinesByDepartment([machine("a", "kinch"), machine("b", "kinch")], tree, "lead");
    expect(groups?.map((group) => group.path)).toEqual(["研发部/开发二组"]);
  });

  it("returns null for a viewer who only sees their own machines or no departments exist", () => {
    expect(groupMachinesByDepartment([machine("a", "kinch")], tree, "kinch")).toBeNull();
    expect(groupMachinesByDepartment([machine("a", "x")], { units: [], members: [], my_unit_id: "" }, "lead")).toBeNull();
  });
});
