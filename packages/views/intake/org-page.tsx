"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type OrgAgent, type OrgImportResult, type OrgUnit } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspaceSlug } from "@multica/core/paths";
import { ORG_IMPORT_PLACEHOLDER, parseOrgImportText } from "./org-import";

interface OrgNode {
  unit: OrgUnit;
  agents: OrgAgent[];
  children: OrgNode[];
}

function buildTree(units: OrgUnit[], agents: OrgAgent[]): OrgNode[] {
  const nodes = new Map<string, OrgNode>();
  for (const unit of units) {
    const own = agents.filter((agent) => agent.unit_id === unit.id);
    nodes.set(unit.id, { unit, agents: own, children: [] });
  }
  const roots: OrgNode[] = [];
  for (const node of nodes.values()) {
    const parent = node.unit.parent_id ? nodes.get(node.unit.parent_id) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  return roots;
}

function OrgRows({ nodes, leadNames, depth = 0 }: { nodes: OrgNode[]; leadNames: Map<string, string>; depth?: number }) {
  return (
    <>
      {nodes.map((node) => (
        <div key={node.unit.id} className="flex flex-col gap-1">
          <div className="flex items-center gap-2 text-sm" style={{ paddingLeft: depth * 16 }}>
            <span className="font-medium">{node.unit.name}</span>
            {node.unit.lead_user_id ? (
              <span className="text-[var(--text-secondary)]">
                {leadNames.get(node.unit.lead_user_id) ?? node.unit.lead_user_id.slice(0, 8)}
              </span>
            ) : null}
          </div>
          {node.agents.map((agent) => (
            <div
              key={agent.id}
              className="text-sm text-[var(--text-secondary)]"
              style={{ paddingLeft: depth * 16 + 16 }}
            >
              {agent.name}
              {agent.can ? ` · ${agent.can}` : ""}
            </div>
          ))}
          <OrgRows nodes={node.children} leadNames={leadNames} depth={depth + 1} />
        </div>
      ))}
    </>
  );
}

function resultLines(result: OrgImportResult): string[] {
  const lines = [
    `新建 ${result.created_units} 个，已存在 ${result.existing_units} 个，挂上 ${result.agents_assigned} 个 Agent`,
  ];
  if (result.unknown_agents.length > 0) {
    lines.push(`找不到这些 Agent：${result.unknown_agents.join("、")}`);
  }
  if (result.unknown_leads.length > 0) {
    lines.push(`找不到这些负责人：${result.unknown_leads.join("、")}`);
  }
  return lines;
}

export function OrgPage() {
  const wsKey = useWorkspaceSlug() ?? "session";
  const workspace = useCurrentWorkspace();
  const workspaceId = workspace?.id ?? "";
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<OrgImportResult | null>(null);
  const [failure, setFailure] = useState("");
  const tree = useQuery({
    queryKey: ["org", wsKey],
    queryFn: () => api.listOrgUnits(),
  });
  const members = useQuery({
    queryKey: ["members", workspaceId],
    queryFn: () => api.listMembers(workspaceId),
    enabled: workspaceId !== "",
  });
  const leadNames = new Map<string, string>();
  for (const member of members.data ?? []) leadNames.set(member.user_id, member.name || member.email);
  const parsed = parseOrgImportText(text);
  const nodes = buildTree(tree.data?.units ?? [], tree.data?.agents ?? []);

  const runImport = async () => {
    if (parsed.rows.length === 0 || parsed.errors.length > 0) return;
    setBusy(true);
    setFailure("");
    try {
      const next = await api.importOrgUnits(parsed.rows);
      setResult(next);
      await queryClient.invalidateQueries({ queryKey: ["org"] });
    } catch (error) {
      setResult(null);
      setFailure(error instanceof Error ? error.message : "导入失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="text-sm">组织架构</div>
      {failure ? <div className="text-sm text-red-600">{failure}</div> : null}
      <textarea
        value={text}
        onChange={(event) => setText(event.target.value)}
        placeholder={ORG_IMPORT_PLACEHOLDER}
        rows={6}
        className="w-full rounded-md border p-3 text-sm"
      />
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => void runImport()}
          disabled={busy || parsed.rows.length === 0 || parsed.errors.length > 0}
          className="h-9 w-fit rounded-md bg-[var(--primary)] px-4 text-sm text-[var(--primary-foreground)] disabled:opacity-50"
        >
          {busy ? "导入中" : `导入 ${parsed.rows.length} 行`}
        </button>
        {parsed.errors.map((error) => (
          <div key={error} className="text-sm text-red-600">{error}</div>
        ))}
      </div>
      {result ? (
        <div className="flex flex-col gap-1 text-sm text-[var(--text-secondary)]">
          {resultLines(result).map((line) => <div key={line}>{line}</div>)}
        </div>
      ) : null}
      {tree.isLoading ? <div className="text-sm text-[var(--text-secondary)]">加载中</div> : null}
      {tree.isError ? <div className="text-sm text-red-600">组织架构加载失败</div> : null}
      {!tree.isLoading && !tree.isError && nodes.length === 0 ? (
        <div className="text-sm text-[var(--text-secondary)]">还没有组织</div>
      ) : null}
      <div className="flex flex-col gap-2">
        <OrgRows nodes={nodes} leadNames={leadNames} />
      </div>
    </div>
  );
}
