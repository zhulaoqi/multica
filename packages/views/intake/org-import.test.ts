// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseOrgImportText } from "./org-import";

describe("parseOrgImportText", () => {
  it("parses path, lead, and agents", () => {
    const { rows, errors } = parseOrgImportText("平台部/支付组 | Lead@Example.com | Mika, Cursor\n  \n数据部");
    expect(errors).toEqual([]);
    expect(rows).toEqual([
      { path: "平台部/支付组", lead_email: "Lead@Example.com", agents: ["Mika", "Cursor"] },
      { path: "数据部", lead_email: "", agents: [] },
    ]);
  });

  it("accepts Chinese separators for agent names", () => {
    const { rows } = parseOrgImportText("平台部 | | Mika、Cursor，Arch");
    expect(rows[0]?.agents).toEqual(["Mika", "Cursor", "Arch"]);
  });

  it("reports the line number of an empty path", () => {
    const { rows, errors } = parseOrgImportText("平台部\n | lead@example.com");
    expect(rows).toHaveLength(1);
    expect(errors).toEqual(["第 2 行：组织路径不能为空"]);
  });

  it("rejects lines with too many segments", () => {
    const { rows, errors } = parseOrgImportText("平台部 | a@example.com | Mika | extra");
    expect(rows).toEqual([]);
    expect(errors[0]).toContain("最多三段");
  });
});
