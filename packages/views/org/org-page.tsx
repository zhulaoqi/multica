"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  EMPTY_ORG_TREE,
  api,
  type OrgAgent,
  type OrgMember,
  type OrgSquad,
  type OrgSyncReport,
  type OrgTree,
} from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, orgSyncStatusOptions, orgTreeOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../i18n";
import {
  agentAvailability,
  buildOrgForest,
  squadAvailability,
  unitPaths,
  type OrgNode,
} from "./org-tree";

const NO_UNIT = "__none";

function useOrgActions() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState("");
  const run = async (action: () => Promise<unknown>, fallback: string) => {
    setFailure("");
    try {
      await action();
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
    } catch (error) {
      setFailure(error instanceof Error && error.message ? error.message : fallback);
    }
  };
  return { run, failure, setFailure };
}

function UnitPicker({
  value,
  choices,
  onChange,
  disabled,
}: {
  value: string;
  choices: Array<{ id: string; path: string }>;
  onChange: (unitId: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("org");
  const items = [{ value: NO_UNIT, label: t(($) => $.unassigned) }, ...choices.map((choice) => ({ value: choice.id, label: choice.path }))];
  return (
    <Select
      items={items}
      value={value || NO_UNIT}
      onValueChange={(next) => onChange(!next || next === NO_UNIT ? "" : next)}
      disabled={disabled}
    >
      <SelectTrigger className="h-8 w-44 text-caption">
        <SelectValue>
          {() =>
            value ? choices.find((choice) => choice.id === value)?.path ?? t(($) => $.move_to) : t(($) => $.unassigned)
          }
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NO_UNIT}>{t(($) => $.unassigned)}</SelectItem>
        {choices.map((choice) => (
          <SelectItem key={choice.id} value={choice.id}>
            {choice.path}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function MemberRow({
  member,
  canMove,
  choices,
  onMove,
}: {
  member: OrgMember;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  return (
    <div className="flex items-center gap-3 py-1.5">
      <ActorAvatar
        name={member.name}
        initials={member.name.charAt(0).toUpperCase()}
        avatarUrl={member.avatar_url ?? undefined}
        size="sm"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-body">{member.name}</div>
        <div className="truncate text-caption text-muted-foreground">{member.email}</div>
      </div>
      {canMove ? (
        <UnitPicker value={member.unit_id} choices={choices} onChange={onMove} />
      ) : null}
    </div>
  );
}

type AvailabilityTone = "waiting" | "working" | "offline" | "attention" | "empty" | "unknown";

/** Small state chip: waiting for collaborative scheduling, busy, offline… */
function StatusChip({ tone }: { tone: AvailabilityTone }) {
  const { t } = useT("org");
  const label = {
    waiting: t(($) => $.waiting_dispatch),
    working: t(($) => $.status_working),
    offline: t(($) => $.status_offline),
    attention: t(($) => $.status_attention),
    empty: t(($) => $.squad_empty),
    unknown: "",
  }[tone];
  if (!label) return null;
  const toneClass =
    tone === "working"
      ? "text-primary"
      : tone === "attention"
        ? "text-destructive"
        : "text-muted-foreground";
  return (
    <span
      className={`shrink-0 rounded-full border border-border/60 px-2 py-0.5 text-caption ${toneClass}`}
    >
      {label}
    </span>
  );
}

function AgentRow({
  agent,
  canMove,
  choices,
  onMove,
}: {
  agent: OrgAgent;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  return (
    <div className="flex items-center gap-3 py-1.5">
      <div className="min-w-0 flex-1">
        <div className="truncate text-body">{agent.name}</div>
        {agent.can ? (
          <div className="truncate text-caption text-muted-foreground">{agent.can}</div>
        ) : null}
      </div>
      <StatusChip tone={agentAvailability(agent.status)} />
      {canMove ? (
        <UnitPicker value={agent.unit_id} choices={choices} onChange={onMove} />
      ) : null}
    </div>
  );
}

function SquadRow({
  squad,
  canMove,
  choices,
  onMove,
}: {
  squad: OrgSquad;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  const { t } = useT("org");
  return (
    <div className="flex items-center gap-3 py-1.5">
      <div className="min-w-0 flex-1">
        <div className="truncate text-body">{squad.name}</div>
        <div className="truncate text-caption text-muted-foreground">
          {t(($) => $.squad_agents, { count: squad.agent_count })}
        </div>
      </div>
      <StatusChip tone={squadAvailability(squad)} />
      {canMove ? (
        <UnitPicker value={squad.unit_id} choices={choices} onChange={onMove} />
      ) : null}
    </div>
  );
}

function TreeRow({
  node,
  selected,
  onSelect,
  depth,
}: {
  node: OrgNode;
  selected: string;
  onSelect: (unitId: string) => void;
  depth: number;
}) {
  return (
    <>
      <button
        type="button"
        aria-current={selected === node.unit.id ? "true" : undefined}
        onClick={() => onSelect(node.unit.id)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-sidebar-accent aria-[current=true]:bg-sidebar-accent"
        style={{ paddingLeft: depth * 14 + 8 }}
      >
        <span className="min-w-0 flex-1 truncate">{node.unit.name}</span>
        <span className="text-caption text-muted-foreground tabular-nums">
          {node.members.length + node.agents.length + node.squads.length}
        </span>
      </button>
      {node.children.map((child) => (
        <TreeRow key={child.unit.id} node={child} selected={selected} onSelect={onSelect} depth={depth + 1} />
      ))}
    </>
  );
}

function UnitDetail({
  node,
  path,
  unitChoices,
  allMembers,
  actions,
}: {
  node: OrgNode;
  path: string;
  unitChoices: Array<{ id: string; path: string; manage: boolean }>;
  allMembers: Array<{ user_id: string; label: string }>;
  actions: ReturnType<typeof useOrgActions>;
}) {
  const { unit, agents, members, squads, children } = node;
  const { t } = useT("org");
  const { run } = actions;
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(unit.name);
  const [confirming, setConfirming] = useState(false);
  const [childName, setChildName] = useState("");
  const leadLabel = unit.lead_user_id
    ? allMembers.find((member) => member.user_id === unit.lead_user_id)?.label ?? unit.lead_user_id.slice(0, 8)
    : "";
  const manageableChoices = unitChoices.filter((choice) => choice.manage);
  const leadItems = [
    { label: t(($) => $.no_lead), value: NO_UNIT },
    ...allMembers.map((member) => ({ label: member.label, value: member.user_id })),
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {renaming ? (
          <>
            <Input value={draft} onChange={(event) => setDraft(event.target.value)} className="h-8 w-52" />
            <Button
              size="sm"
              onClick={() => {
                if (!draft.trim()) return;
                void run(() => api.updateOrgUnit(unit.id, { name: draft.trim() }), t(($) => $.action_failed));
                setRenaming(false);
              }}
            >
              {t(($) => $.save)}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => { setDraft(unit.name); setRenaming(false); }}>
              {t(($) => $.cancel)}
            </Button>
          </>
        ) : (
          <>
            <h2 className="text-heading">{unit.name}</h2>
            <span className="text-caption text-muted-foreground">{path}</span>
            {unit.can_manage ? (
              <Button size="sm" variant="ghost" onClick={() => setRenaming(true)}>
                {t(($) => $.rename)}
              </Button>
            ) : null}
          </>
        )}
        {unit.can_manage ? (
          <>
            <Select
              items={leadItems}
              value={unit.lead_user_id || NO_UNIT}
              onValueChange={(next) =>
                void run(
                  () =>
                    api.updateOrgUnit(unit.id, {
                      lead_user_id: !next || next === NO_UNIT ? "" : next,
                    }),
                  t(($) => $.action_failed),
                )
              }
            >
              <SelectTrigger className="h-8 w-44 text-caption">
                <SelectValue>{() => leadLabel || t(($) => $.no_lead)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {leadItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {confirming ? (
              <>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    setConfirming(false);
                    void run(() => api.deleteOrgUnit(unit.id), t(($) => $.action_failed));
                  }}
                >
                  {t(($) => $.confirm_delete)}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                  {t(($) => $.cancel)}
                </Button>
              </>
            ) : (
              <Button size="sm" variant="ghost" onClick={() => setConfirming(true)}>
                {t(($) => $.delete)}
              </Button>
            )}
          </>
        ) : null}
      </div>

      <section className="flex flex-col gap-1">
        <h3 className="text-body font-medium">{t(($) => $.members)}</h3>
        {members.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.no_members)}</p>
        ) : (
          members.map((member) => (
            <MemberRow
              key={member.user_id}
              member={member}
              canMove={unit.can_manage}
              choices={manageableChoices}
              onMove={(unitId) => void run(() => api.setOrgMemberUnit(member.user_id, unitId), t(($) => $.action_failed))}
            />
          ))
        )}
      </section>

      <section className="flex flex-col gap-1">
        <h3 className="text-body font-medium">{t(($) => $.agents)}</h3>
        {agents.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.no_agents)}</p>
        ) : (
          agents.map((agent) => (
            <AgentRow
              key={agent.id}
              agent={agent}
              canMove={unit.can_manage}
              choices={manageableChoices}
              onMove={(unitId) => void run(() => api.setOrgAgentUnit(agent.id, unitId), t(($) => $.action_failed))}
            />
          ))
        )}
      </section>

      <section className="flex flex-col gap-1">
        <h3 className="text-body font-medium">{t(($) => $.squads)}</h3>
        {squads.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.no_squads)}</p>
        ) : (
          squads.map((squad) => (
            <SquadRow
              key={squad.id}
              squad={squad}
              canMove={unit.can_manage}
              choices={manageableChoices}
              onMove={(unitId) => void run(() => api.setOrgSquadUnit(squad.id, unitId), t(($) => $.action_failed))}
            />
          ))
        )}
      </section>

      <section className="flex flex-col gap-1">
        <h3 className="text-body font-medium">{t(($) => $.create_child)}</h3>
        {children.length > 0 ? (
          <p className="text-caption text-muted-foreground">
            {children.map((child) => child.unit.name).join("、")}
          </p>
        ) : null}
        {unit.can_manage ? (
          <div className="flex items-center gap-2">
            <Input
              value={childName}
              onChange={(event) => setChildName(event.target.value)}
              placeholder={t(($) => $.unit_name_placeholder)}
              className="h-8 w-52"
            />
            <Button
              size="sm"
              disabled={!childName.trim()}
              onClick={() => {
                const name = childName.trim();
                if (!name) return;
                void run(() => api.createOrgUnit({ name, parent_id: unit.id }), t(($) => $.action_failed));
                setChildName("");
              }}
            >
              {t(($) => $.new_unit)}
            </Button>
          </div>
        ) : null}
      </section>
    </div>
  );
}

/** A real directory sync moves hundreds of names; show the head of each list
 * and count the rest so the report stays readable. */
const SYNC_REPORT_NAME_LIMIT = 8;

function summariseNames(names: string[]): string {
  const shown = names.slice(0, SYNC_REPORT_NAME_LIMIT);
  const hidden = names.length - shown.length;
  return hidden > 0 ? `${shown.join(", ")} +${hidden}` : shown.join(", ");
}

/** SyncReport summarises the last directory sync in the change lists the
 * server returns, so an admin sees exactly what moved without diffing. */
function SyncReport({ report }: { report: OrgSyncReport }) {
  const { t } = useT("org");
  const lines: Array<[string, string[]]> = [
    [t(($) => $.sync_report_created), report.created_units],
    [t(($) => $.sync_report_updated), report.updated_units],
    [t(($) => $.sync_report_leads), report.leads_assigned],
    [t(($) => $.sync_report_added), report.members_added],
    [t(($) => $.sync_report_moved), report.members_moved],
    [t(($) => $.sync_report_no_email), report.members_without_email],
    [t(($) => $.sync_report_detached_departments), report.detached_departments],
    [t(($) => $.sync_report_detached_users), report.detached_users],
  ];
  const visible = lines.filter(([, names]) => names.length > 0);
  if (visible.length === 0) {
    return <p className="mb-4 text-caption text-muted-foreground">{t(($) => $.sync_nothing_changed)}</p>;
  }
  return (
    <div className="mb-4 flex flex-col gap-0.5 text-caption text-muted-foreground">
      {visible.map(([label, names]) => (
        <p key={label}>
          {t(($) => $.sync_report_line, { label, names: summariseNames(names) })}
        </p>
      ))}
    </div>
  );
}

/** Plain members land on their own department, so the page answers "where do
 * I sit, what does my team do, and which squads are waiting" before the tree. */
function MyOrgCard({
  unitId,
  data,
  paths,
  leadLabel,
  onSelect,
}: {
  unitId: string;
  data: OrgTree;
  paths: Map<string, string>;
  leadLabel: string;
  onSelect: (unitId: string) => void;
}) {
  const { t } = useT("org");
  const unit = data.units.find((entry) => entry.id === unitId);
  if (!unit) {
    return <p className="mb-4 text-caption text-muted-foreground">{t(($) => $.my_org_empty)}</p>;
  }
  const agents = data.agents.filter((agent) => agent.unit_id === unitId);
  const squads = data.squads.filter((squad) => squad.unit_id === unitId);
  return (
    <section className="mb-6 flex flex-col gap-2 rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-body font-medium">{t(($) => $.my_org)}</h2>
        <button
          type="button"
          onClick={() => onSelect(unitId)}
          className="text-body text-primary hover:underline"
        >
          {paths.get(unitId) ?? unit.name}
        </button>
        {leadLabel ? (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.lead)} · {leadLabel}
          </span>
        ) : null}
      </div>
      {squads.length === 0 && agents.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.my_org_quiet)}</p>
      ) : null}
      {squads.map((squad) => (
        <SquadRow key={squad.id} squad={squad} canMove={false} choices={[]} onMove={() => undefined} />
      ))}
      {agents.map((agent) => (
        <AgentRow key={agent.id} agent={agent} canMove={false} choices={[]} onMove={() => undefined} />
      ))}
    </section>
  );
}

export function OrgPage() {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const tree = useQuery(orgTreeOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const syncStatus = useQuery(orgSyncStatusOptions(wsId));
  const [selected, setSelected] = useState("");
  const [rootName, setRootName] = useState("");
  const [syncing, setSyncing] = useState(false);
  const actions = useOrgActions();
  const { run, failure, setFailure } = actions;
  const data: OrgTree = tree.data ?? EMPTY_ORG_TREE;
  const paths = unitPaths(data.units);
  const unitChoices = data.units
    .map((unit) => ({ id: unit.id, path: paths.get(unit.id) ?? unit.name, manage: unit.can_manage }))
    .sort((a, b) => a.path.localeCompare(b.path));
  const forest = buildOrgForest(data);
  // Landing on the caller's own department keeps plain members oriented; an
  // explicit click (including the unassigned pseudo-node) always wins.
  const activeSelected = selected || data.my_unit_id;
  const selectedNode = findNode(forest.roots, activeSelected);
  const allMembers = (members.data ?? []).map((member) => ({
    user_id: member.user_id,
    label: member.name || member.email,
  }));
  const canAnyManage = data.can_manage || data.units.some((unit) => unit.can_manage);
  const moveChoices = unitChoices.filter((choice) => choice.manage);
  const lastRun = syncStatus.data?.last_run ?? null;
  const runSync = async () => {
    setFailure("");
    setSyncing(true);
    try {
      const report = await api.syncOrgFromFeishu();
      queryClient.setQueryData(workspaceKeys.orgSync(wsId), { configured: true, last_run: report });
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    } catch (error) {
      setFailure(error instanceof Error && error.message ? error.message : t(($) => $.sync_failed));
    } finally {
      setSyncing(false);
    }
  };

  if (tree.isError) {
    return <p className="p-6 text-body text-muted-foreground">{t(($) => $.load_failed)}</p>;
  }

  return (
    <div className="flex h-full min-h-0">
      <aside className="flex w-72 shrink-0 flex-col gap-2 overflow-y-auto border-r p-3">
        <h1 className="px-2 text-heading">{t(($) => $.title)}</h1>
        {data.can_manage ? (
          <div className="flex items-center gap-2 px-1">
            <Input
              value={rootName}
              onChange={(event) => setRootName(event.target.value)}
              placeholder={t(($) => $.unit_name_placeholder)}
              className="h-8"
            />
            <Button
              size="sm"
              disabled={!rootName.trim()}
              onClick={() => {
                const name = rootName.trim();
                if (!name) return;
                void run(() => api.createOrgUnit({ name }), t(($) => $.action_failed));
                setRootName("");
              }}
            >
              {t(($) => $.create_root)}
            </Button>
          </div>
        ) : null}
        {canAnyManage && data.units.length === 0 && !tree.isLoading ? (
          <p className="px-2 text-caption text-muted-foreground">{t(($) => $.no_units)}</p>
        ) : null}
        {forest.roots.map((node) => (
          <TreeRow key={node.unit.id} node={node} selected={activeSelected} onSelect={setSelected} depth={0} />
        ))}
        {forest.unassignedMembers.length + forest.unassignedAgents.length + forest.unassignedSquads.length > 0 ? (
          <button
            type="button"
            aria-current={activeSelected === NO_UNIT ? "true" : undefined}
            onClick={() => setSelected(NO_UNIT)}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-sidebar-accent aria-[current=true]:bg-sidebar-accent"
          >
            <span className="min-w-0 flex-1 truncate">{t(($) => $.unassigned)}</span>
            <span className="text-caption text-muted-foreground tabular-nums">
              {forest.unassignedMembers.length + forest.unassignedAgents.length + forest.unassignedSquads.length}
            </span>
          </button>
        ) : null}
      </aside>
      <main className="min-w-0 flex-1 overflow-y-auto p-6">
        {data.can_manage ? (
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <Button
              size="sm"
              disabled={!syncStatus.data?.configured || syncing}
              onClick={() => void runSync()}
            >
              {syncing ? t(($) => $.syncing) : t(($) => $.sync_from_feishu)}
            </Button>
            <span className="text-caption text-muted-foreground">
              {syncStatus.data?.configured
                ? lastRun?.synced_at
                  ? t(($) => $.last_synced, { time: new Date(lastRun.synced_at).toLocaleString() })
                  : t(($) => $.never_synced)
                : t(($) => $.sync_not_configured)}
            </span>
          </div>
        ) : null}
        {data.can_manage && lastRun ? <SyncReport report={lastRun} /> : null}
        {failure ? <p className="mb-3 text-body text-destructive">{failure}</p> : null}
        {!canAnyManage ? (
          <>
            <MyOrgCard
              unitId={data.my_unit_id}
              data={data}
              paths={paths}
              leadLabel={
                allMembers.find((member) =>
                  member.user_id === data.units.find((unit) => unit.id === data.my_unit_id)?.lead_user_id,
                )?.label ?? ""
              }
              onSelect={setSelected}
            />
            <p className="mb-3 text-caption text-muted-foreground">{t(($) => $.read_only)}</p>
          </>
        ) : null}
        {selectedNode ? (
          <UnitDetail
            node={selectedNode}
            path={paths.get(selectedNode.unit.id) ?? selectedNode.unit.name}
            unitChoices={unitChoices}
            allMembers={allMembers}
            actions={actions}
          />
        ) : activeSelected === NO_UNIT ? (
          <div className="flex flex-col gap-4">
            <h2 className="text-heading">{t(($) => $.unassigned)}</h2>
            <section className="flex flex-col gap-1">
              <h3 className="text-body font-medium">{t(($) => $.members)}</h3>
              {forest.unassignedMembers.map((member) => (
                <MemberRow
                  key={member.user_id}
                  member={member}
                  canMove={moveChoices.length > 0}
                  choices={moveChoices}
                  onMove={(unitId) => void run(() => api.setOrgMemberUnit(member.user_id, unitId), t(($) => $.action_failed))}
                />
              ))}
            </section>
            <section className="flex flex-col gap-1">
              <h3 className="text-body font-medium">{t(($) => $.agents)}</h3>
              {forest.unassignedAgents.map((agent) => (
                <AgentRow
                  key={agent.id}
                  agent={agent}
                  canMove={moveChoices.length > 0}
                  choices={moveChoices}
                  onMove={(unitId) => void run(() => api.setOrgAgentUnit(agent.id, unitId), t(($) => $.action_failed))}
                />
              ))}
            </section>
            <section className="flex flex-col gap-1">
              <h3 className="text-body font-medium">{t(($) => $.squads)}</h3>
              {forest.unassignedSquads.map((squad) => (
                <SquadRow
                  key={squad.id}
                  squad={squad}
                  canMove={moveChoices.length > 0}
                  choices={moveChoices}
                  onMove={(unitId) => void run(() => api.setOrgSquadUnit(squad.id, unitId), t(($) => $.action_failed))}
                />
              ))}
            </section>
          </div>
        ) : forest.roots.length > 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.pick_unit)}</p>
        ) : null}
      </main>
    </div>
  );
}

function findNode(nodes: OrgNode[], unitId: string): OrgNode | null {
  for (const node of nodes) {
    if (node.unit.id === unitId) return node;
    const found = findNode(node.children, unitId);
    if (found) return found;
  }
  return null;
}
