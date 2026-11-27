// @vitest-environment node
import { describe, expect, it } from "vitest";
import { visibleHierarchy } from "./org-hierarchy";

const units = [
  { id: "rd", name: "研发部", position: 0 },
  { id: "aihub", name: "AIHUB", parent_id: "rd", position: 0 },
  { id: "bi", name: "BI中台", parent_id: "rd", position: 1 },
  { id: "rd2", name: "研发二组", parent_id: "rd", position: 2 },
  { id: "sales", name: "销售部", position: 1 },
];

describe("visibleHierarchy", () => {
  it("keeps the ancestors of every staffed unit", () => {
    const hierarchy = visibleHierarchy(units, new Set(["aihub", "rd2"]));

    expect(hierarchy.roots.map((unit) => unit.id)).toEqual(["rd"]);
    expect(
      (hierarchy.childrenOf.get("rd") ?? []).map((unit) => unit.id),
    ).toEqual(["aihub", "rd2"]);
    expect(hierarchy.rootOf.get("aihub")).toBe("rd");
    expect(hierarchy.rootOf.get("rd2")).toBe("rd");
    expect(hierarchy.pathOf.get("rd2")).toBe("研发部/研发二组");
    expect(hierarchy.visibleIds.has("bi")).toBe(false);
    expect(hierarchy.visibleIds.has("sales")).toBe(false);
  });

  it("treats a missing parent as a root", () => {
    const hierarchy = visibleHierarchy(
      [{ id: "child", name: "Child", parent_id: "gone" }],
      new Set(["child"]),
    );

    expect(hierarchy.roots.map((unit) => unit.id)).toEqual(["child"]);
    expect(hierarchy.pathOf.get("child")).toBe("Child");
  });

  it("breaks parent cycles deterministically", () => {
    const hierarchy = visibleHierarchy(
      [
        { id: "a", name: "A", parent_id: "b" },
        { id: "b", name: "B", parent_id: "a" },
      ],
      new Set(["a"]),
    );

    expect(hierarchy.roots.map((unit) => unit.id)).toEqual(["a"]);
    expect(hierarchy.rootOf.get("b")).toBe("a");
    expect(hierarchy.pathOf.get("b")).toBe("A/B");
  });
});
