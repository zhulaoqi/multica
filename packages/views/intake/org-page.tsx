"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { EMPTY_ORG_TREE, api, type OrgAgent, type OrgImportResult, type OrgTree, type OrgUnit } from "@multica/core/api";
import { useCurrentWorkspace, useWorkspaceSlug } from "@multica/core/paths";
import { ORG_IMPORT_PLACEHOLDER, parseOrgImportText } from "./org-import";

interface OrgNode {
  unit: OrgUnit;
  agents: OrgAgent[];
  children: OrgNode[];
}

interface Manage {
  members: Array<{ user_id: string; label: string }>;
  unitChoices: Array<{ id: string; path: string }>;
  rename: (unitId: string, name: string) => void;
  setLead: (unitId: string, userId: string) => void;
  remove: (unitId: string) => void;
  moveAgent: (agentId: string, unitId: string) => void;
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

function UnitHeader({
  unit,
  leadNames,
  manage,
  depth,
}: {
  unit: OrgUnit;
  leadNames: Map<string, string>;
  manage?: Manage;
  depth: number;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(unit.name);
  const [confirming, setConfirming] = useState(false);
  const leadLabel = unit.lead_user_id
    ? leadNames.get(unit.lead_user_id) ?? unit.lead_user_id.slice(0, 8)
    : "";

  if (!manage) {
    return (
      <div className="flex items-center gap-2 text-sm" style={{ paddingLeft: depth * 16 }}>
        <span className="font-medium">{unit.name}</span>
        {leadLabel ? <span className="text-[var(--text-secondary)]">{leadLabel}</span> : null}
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm" style={{ paddingLeft: depth * 16 }}>
      {editing ? (
        <>
          <input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            className="h-7 w-40 rounded-md border px-2 text-sm"
          />
          <button
            type="button"
            onClick={() => {
              if (!draft.trim()) return;
              manage.rename(unit.id, draft.trim());
              setEditing(false);
            }}
          >
            保存
          </button>
          <button type="button" onClick={() => { setDraft(unit.name); setEditing(false); }}>取消</button>
        </>
      ) : (
        <>
          <span className="font-medium">{unit.name}</span>
          <button type="button" className="text-[var(--text-secondary)]" onClick={() => setEditing(true)}>改名</button>
        </>
      )}
      <select
        value={unit.lead_user_id ?? ""}
        onChange={(event) => manage.setLead(unit.id, event.target.value)}
        className="h-7 rounded-md border px-2 text-sm text-[var(--text-secondary)]"
      >
        <option value="">负责人</option>
        {manage.members.map((member) => (
          <option key={member.user_id} value={member.user_id}>{member.label}</option>
        ))}
      </select>
      {confirming ? (
        <>
          <button type="button" className="text-red-600" onClick={() => manage.remove(unit.id)}>确认删除</button>
          <button type="button" onClick={() => setConfirming(false)}>取消</button>
        </>
      ) : (
        <button type="button" className="text-[var(--text-secondary)]" onClick={() => setConfirming(true)}>删除</button>
      )}
    </div>
  );
}

function OrgRows({
  nodes,
  leadNames,
  manage,
  unitChoices,
  depth = 0,
}: {
  nodes: OrgNode[];
  leadNames: Map<string, string>;
  manage?: Manage;
  unitChoices: Array<{ id: string; path: string }>;
  depth?: number;
}) {
  return (
    <>
      {nodes.map((node) => (
        <div key={node.unit.id} className="flex flex-col gap-1">
          <UnitHeader unit={node.unit} leadNames={leadNames} manage={manage} depth={depth} />
          {node.agents.map((agent) => (
            <AgentLine
              key={agent.id}
              agent={agent}
              unitChoices={unitChoices}
              depth={depth}
              onChange={manage ? (unitId) => manage.moveAgent(agent.id, unitId) : undefined}
            />
          ))}
          <OrgRows
            nodes={node.children}
            leadNames={leadNames}
            manage={manage}
            unitChoices={unitChoices}
            depth={depth + 1}
          />
        </div>
      ))}
    </>
  );
}

function UnitCreator({
  unitChoices,
  onCreate,
}: {
  unitChoices: Array<{ id: string; path: string }>;
  onCreate: (name: string, parentId: string) => void;
}) {
  const [name, setName] = useState("");
  const [parentId, setParentId] = useState("");
  return (
    <div className="flex items-center gap-2">
      <input
        value={name}
        onChange={(event) => setName(event.target.value)}
        placeholder="新部门名称"
        className="h-9 w-48 rounded-md border px-3 text-sm"
      />
      <select
        value={parentId}
        onChange={(event) => setParentId(event.target.value)}
        className="h-9 rounded-md border px-3 text-sm"
      >
        <option value="">挂在根上</option>
        {unitChoices.map((unit) => (
          <option key={unit.id} value={unit.id}>{unit.path}</option>
        ))}
      </select>
      <button
        type="button"
        disabled={!name.trim()}
        onClick={() => {
          onCreate(name.trim(), parentId);
          setName("");
        }}
        className="h-9 w-fit rounded-md border px-4 text-sm disabled:opacity-50"
      >
        新建部门
      </button>
    </div>
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
  const data: OrgTree = tree.data ?? EMPTY_ORG_TREE;
  const leadNames = new Map<string, string>();
  const memberChoices: Array<{ user_id: string; label: string }> = [];
  for (const member of members.data ?? []) {
    const label = member.name || member.email;
    leadNames.set(member.user_id, label);
    memberChoices.push({ user_id: member.user_id, label });
  }
  const parsed = parseOrgImportText(text);
  const paths = unitPaths(data.units);
  const unitChoices = data.units
    .map((unit) => ({ id: unit.id, path: paths.get(unit.id) ?? unit.name }))
    .sort((a, b) => a.path.localeCompare(b.path));
  const { roots, unassigned } = buildTree(data.units, data.agents);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["org"] });
  const run = async (action: () => Promise<unknown>, failureText: string) => {
    setFailure("");
    try {
      await action();
      await refresh();
    } catch (error) {
      setFailure(error instanceof Error ? error.message : failureText);
    }
  };
  const manage: Manage | undefined = data.can_manage
    ? {
        members: memberChoices,
        unitChoices,
        rename: (unitId, name) => void run(() => api.updateOrgUnit(unitId, { name }), "改名失败"),
        setLead: (unitId, userId) => void run(() => api.updateOrgUnit(unitId, { lead_user_id: userId }), "设置负责人失败"),
        remove: (unitId) => void run(() => api.deleteOrgUnit(unitId), "删除部门失败"),
        moveAgent: (agentId, unitId) => void run(() => api.setOrgAgentUnit(agentId, unitId), "调整归属失败"),
      }
    : undefined;

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

  return (
    <div className="flex flex-col gap-3">
      <div className="text-sm">组织架构</div>
      {failure ? <div className="text-sm text-red-600">{failure}</div> : null}
      {manage ? (
        <UnitCreator
          unitChoices={unitChoices}
          onCreate={(name, parentId) =>
            void run(() => api.createOrgUnit({ name, parent_id: parentId || undefined }), "新建部门失败")
          }
        />
      ) : null}
      {manage ? (
        <>
          <textarea
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder={ORG_IMPORT_PLACEHOLDER}
            rows={4}
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
        </>
      ) : null}
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
        <OrgRows nodes={roots} leadNames={leadNames} manage={manage} unitChoices={unitChoices} />
        {unassigned.length > 0 ? (
          <div className="flex flex-col gap-1">
            <div className="text-sm font-medium">未分配</div>
            {unassigned.map((agent) => (
              <AgentLine
                key={agent.id}
                agent={agent}
                unitChoices={unitChoices}
                depth={0}
                onChange={manage ? (unitId) => manage.moveAgent(agent.id, unitId) : undefined}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
