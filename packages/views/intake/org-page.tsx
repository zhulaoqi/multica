"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type OrgAgent, type OrgImportResult, type OrgTree, type OrgUnit } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspaceSlug } from "@multica/core/paths";
import { ORG_IMPORT_PLACEHOLDER, parseOrgImportText } from "./org-import";

interface OrgNode {
  unit: OrgUnit;
  agents: OrgAgent[];
  children: OrgNode[];
}

function unitPaths(units: OrgUnit[]): Map<string, string> {
  const byID = new Map(units.map((unit) => [unit.id, unit]));
  const paths = new Map<string, string>();
  const resolve = (unit: OrgUnit, seen: Set<string>): string => {
    const known = paths.get(unit.id);
    if (known) return known;
    if (seen.has(unit.id)) return unit.name;
    seen.add(unit.id);
    const parent = unit.parent_id ? byID.get(unit.parent_id) : undefined;
    const path = parent ? `${resolve(parent, seen)}/${unit.name}` : unit.name;
    paths.set(unit.id, path);
    return path;
  };
  for (const unit of units) resolve(unit, new Set());
  return paths;
}

function buildTree(units: OrgUnit[], agents: OrgAgent[]): { roots: OrgNode[]; unassigned: OrgAgent[] } {
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
  const unassigned = agents.filter((agent) => !agent.unit_id || !nodes.has(agent.unit_id));
  return { roots, unassigned };
}

function AgentLine({
  agent,
  unitChoices,
  depth,
  onChange,
}: {
  agent: OrgAgent;
  unitChoices: Array<{ id: string; path: string }>;
  depth: number;
  onChange?: (unitId: string) => void;
}) {
  return (
    <div
      className="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
      style={{ paddingLeft: depth * 16 + 16 }}
    >
      <span>
        {agent.name}
        {agent.can ? ` · ${agent.can}` : ""}
      </span>
      {onChange ? (
        <select
          value={agent.unit_id ?? ""}
          onChange={(event) => onChange(event.target.value)}
          className="h-7 rounded-md border px-2 text-sm"
        >
          <option value="">未分配</option>
          {unitChoices.map((unit) => (
            <option key={unit.id} value={unit.id}>{unit.path}</option>
          ))}
        </select>
      ) : null}
    </div>
  );
}

function OrgRows({
  nodes,
  leadNames,
  unitChoices,
  onMove,
  depth = 0,
}: {
  nodes: OrgNode[];
  leadNames: Map<string, string>;
  unitChoices: Array<{ id: string; path: string }>;
  onMove?: (agentId: string, unitId: string) => void;
  depth?: number;
}) {
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
            <AgentLine
              key={agent.id}
              agent={agent}
              unitChoices={unitChoices}
              depth={depth}
              onChange={onMove ? (unitId) => onMove(agent.id, unitId) : undefined}
            />
          ))}
          <OrgRows
            nodes={node.children}
            leadNames={leadNames}
            unitChoices={unitChoices}
            onMove={onMove}
            depth={depth + 1}
          />
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
  const data: OrgTree = tree.data ?? { units: [], agents: [], can_manage: false };
  const leadNames = new Map<string, string>();
  for (const member of members.data ?? []) leadNames.set(member.user_id, member.name || member.email);
  const parsed = parseOrgImportText(text);
  const paths = unitPaths(data.units);
  const unitChoices = data.units
    .map((unit) => ({ id: unit.id, path: paths.get(unit.id) ?? unit.name }))
    .sort((a, b) => a.path.localeCompare(b.path));
  const { roots, unassigned } = buildTree(data.units, data.agents);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["org"] });
  const moveAgent = async (agentId: string, unitId: string) => {
    setFailure("");
    try {
      await api.setOrgAgentUnit(agentId, unitId);
      await refresh();
    } catch (error) {
      setFailure(error instanceof Error ? error.message : "调整归属失败");
    }
  };
  const runImport = async () => {
    if (parsed.rows.length === 0 || parsed.errors.length > 0) return;
    setBusy(true);
    setFailure("");
    try {
      const next = await api.importOrgUnits(parsed.rows);
      setResult(next);
      await refresh();
    } catch (error) {
      setResult(null);
      setFailure(error instanceof Error ? error.message : "导入失败");
    } finally {
      setBusy(false);
    }
  };
  const canManage = data.can_manage;

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
      {!tree.isLoading && !tree.isError && data.units.length === 0 ? (
        <div className="text-sm text-[var(--text-secondary)]">还没有组织</div>
      ) : null}
      <div className="flex flex-col gap-2">
        <OrgRows
          nodes={roots}
          leadNames={leadNames}
          unitChoices={unitChoices}
          onMove={canManage ? (agentId, unitId) => void moveAgent(agentId, unitId) : undefined}
        />
        {unassigned.length > 0 ? (
          <div className="flex flex-col gap-1">
            <div className="text-sm font-medium">未分配</div>
            {unassigned.map((agent) => (
              <AgentLine
                key={agent.id}
                agent={agent}
                unitChoices={unitChoices}
                depth={0}
                onChange={canManage ? (unitId) => void moveAgent(agent.id, unitId) : undefined}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
