import type { OrgImportRow } from "@multica/core/api";

/** One line is `部门/组 | lead@example.com | Agent名,Agent名`; lead and agents are optional. */
export const ORG_IMPORT_PLACEHOLDER = [
  "平台部/支付组 | lead@example.com | Mika,Cursor",
  "平台部/结算组 | | Arch",
  "数据部",
].join("\n");

export interface OrgImportParse {
  rows: OrgImportRow[];
  errors: string[];
}

export function parseOrgImportText(text: string): OrgImportParse {
  const rows: OrgImportRow[] = [];
  const errors: string[] = [];
  text.split("\n").forEach((raw, index) => {
    const line = raw.trim();
    if (!line) return;
    const parts = line.split("|").map((part) => part.trim());
    if (parts.length > 3) {
      errors.push(`第 ${index + 1} 行：最多三段（路径 | 负责人邮箱 | Agent）`);
      return;
    }
    const path = parts[0] ?? "";
    if (!path) {
      errors.push(`第 ${index + 1} 行：组织路径不能为空`);
      return;
    }
    const agents = (parts[2] ?? "")
      .split(/[,，、]/)
      .map((name) => name.trim())
      .filter(Boolean);
    rows.push({ path, lead_email: parts[1] ?? "", agents });
  });
  return { rows, errors };
}
