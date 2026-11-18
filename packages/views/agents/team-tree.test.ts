// @vitest-environment node

import { describe, expect, it } from "vitest";
import { countDepartmentRows, departmentForest, type DepartmentNode } from "./team-tree";

function unit(id: string, name: string, parentId = "", position = 0, lead = "") {
  return { id, name, parent_id: parentId, lead_user_id: lead, can_manage: true, description: "", position };
}

function member(userId: string, unitId: string) {
  return { user_id: userId, unit_id: unitId, name: userId, email: "", avatar_url: null, in_project: true };
}

const tree = {
  units: [
    unit("rd", "研发部", "", 0, "henry"),
    unit("dev1", "开发一组", "rd", 0),
    unit("dev2", "开发二组", "rd", 1),
    unit("algo", "算法组", "rd", 2),
  ],
  members: [member("henry", "rd"), member("kinch", "dev2"), member("phil", "dev1")],
};

type Row = { id: string; owner: string };

function shape(node: DepartmentNode<Row>): unknown {
  return [node.name, node.rows.map((row) => row.id), node.children.map(shape)];
}

describe("departmentForest", () => {
  it("nests departments, keeps ancestors, drops empty branches", () => {
    const rows: Row[] = [
      { id: "coding", owner: "kinch" },
      { id: "mika", owner: "kinch" },
      { id: "loose", owner: "stranger" },
    ];
    const forest = departmentForest(rows, (row) => row.owner, tree);
    expect(forest.map(shape)).toEqual([
      ["研发部", [], [["开发二组", ["coding", "mika"], []]]],
      ["", ["loose"], []],
    ]);
    expect(forest[0]?.leadUserId).toBe("henry");
    expect(countDepartmentRows(forest[0]!)).toBe(2);
  });

  it("keeps sibling order by position", () => {
    const rows: Row[] = [
      { id: "b", owner: "kinch" },
      { id: "a", owner: "phil" },
      { id: "c", owner: "henry" },
    ];
    const [root] = departmentForest(rows, (row) => row.owner, tree);
    expect(root?.rows.map((row) => row.id)).toEqual(["c"]);
    expect(root?.children.map((child) => child.name)).toEqual(["开发一组", "开发二组"]);
  });
});
