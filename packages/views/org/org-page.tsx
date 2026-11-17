"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { EMPTY_ORG_TREE, api, type OrgAgent, type OrgSyncReport, type OrgTree } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  orgSyncStatusOptions,
  orgTreeOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../i18n";
import { buildOrgForest, memberTwin, unitPaths } from "./org-tree";
import { AgentCard, MemberRow, NO_UNIT, OrgCanvas, SquadCard } from "./org-canvas";

function useOrgActions() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState("");
  const run = async (action: () => Promise<unknown>, fallback: string) => {
    setFailure("");
    try {
      await action();
      await queryClient.invalidateQueries({
        queryKey: workspaceKeys.org(wsId),
      });
    } catch (error) {
      setFailure(error instanceof Error && error.message ? error.message : fallback);
    }
  };
  return { run, failure, setFailure };
}

function TreeRow({
  node,
  selected,
  onSelect,
  depth,
}: {
  node: ReturnType<typeof buildOrgForest>["roots"][number];
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
          {t(($) => $.sync_report_line, {
            label,
            names: summariseNames(names),
          })}
        </p>
      ))}
    </div>
  );
}

function canSyncDirectory(role: string | undefined): boolean {
  return role === "owner" || role === "admin";
}

export function OrgPage() {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id);
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
    .map((unit) => ({
      id: unit.id,
      path: paths.get(unit.id) ?? unit.name,
      manage: unit.can_manage,
    }))
    .sort((a, b) => a.path.localeCompare(b.path));
  const forest = buildOrgForest(data);
  // Landing on the caller's own department keeps plain members oriented; an
  // explicit click (including the unassigned pseudo-node) always wins.
  const activeSelected =
    selected ||
    data.my_unit_id ||
    (!data.can_manage && forest.unassignedSquads.length > 0 ? NO_UNIT : "");
  const selectedNode = findNode(forest.roots, activeSelected);
  const allMembers = (members.data ?? []).map((member) => ({
    user_id: member.user_id,
    label: member.name || member.email,
  }));
  // The server's can_manage flag is the authority: workspace owners, workspace
  // admins, and the operator account. The member-role check covers the same
  // people when their row is in the roster. Department leads stay out.
  const myRole = (members.data ?? []).find((member) => member.user_id === userId)?.role;
  const maySyncDirectory = data.can_manage || canSyncDirectory(myRole);
  const canAnyManage = data.can_manage || data.units.some((unit) => unit.can_manage);
  const moveChoices = unitChoices.filter((choice) => choice.manage);
  // Unassigned people keep their twins on their own row; those agents leave
  // the unassigned agent list.
  const unassignedTwins = new Map<string, OrgAgent>();
  for (const member of forest.unassignedMembers) {
    const twin = memberTwin(forest.unassignedAgents, member.user_id);
    if (twin) unassignedTwins.set(member.user_id, twin);
  }
  const unassignedTwinIds = new Set([...unassignedTwins.values()].map((twin) => twin.id));
  const unassignedAgents = forest.unassignedAgents.filter((agent) => !unassignedTwinIds.has(agent.id));
  const lastRun = syncStatus.data?.last_run ?? null;
  const runSync = async () => {
    if (!maySyncDirectory) return;
    setFailure("");
    setSyncing(true);
    try {
      const report = await api.syncOrgFromFeishu();
      queryClient.setQueryData(workspaceKeys.orgSync(wsId), {
        configured: true,
        last_run: report,
      });
      await queryClient.invalidateQueries({
        queryKey: workspaceKeys.org(wsId),
      });
      await queryClient.invalidateQueries({
        queryKey: workspaceKeys.members(wsId),
      });
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
                void run(
                  () => api.createOrgUnit({ name }),
                  t(($) => $.action_failed),
                );
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
        {maySyncDirectory ? (
          <div className="mx-auto mb-4 flex max-w-3xl flex-wrap items-center gap-3">
            <Button size="sm" disabled={!syncStatus.data?.configured || syncing} onClick={() => void runSync()}>
              {syncing ? t(($) => $.syncing) : t(($) => $.sync_from_feishu)}
            </Button>
            <span className="text-caption text-muted-foreground">
              {syncStatus.data?.configured
                ? lastRun?.synced_at
                  ? t(($) => $.last_synced, {
                      time: new Date(lastRun.synced_at).toLocaleString(),
                    })
                  : t(($) => $.never_synced)
                : t(($) => $.sync_not_configured)}
            </span>
          </div>
        ) : null}
        {maySyncDirectory && lastRun ? (
          <div className="mx-auto max-w-3xl">
            <SyncReport report={lastRun} />
          </div>
        ) : null}
        {failure ? <p className="mb-3 text-center text-body text-destructive">{failure}</p> : null}
        {!canAnyManage ? (
          <p className="mx-auto mb-4 max-w-3xl text-caption text-muted-foreground">{t(($) => $.read_only)}</p>
        ) : null}
        {selectedNode ? (
          <OrgCanvas
            node={selectedNode}
            path={paths.get(selectedNode.unit.id) ?? selectedNode.unit.name}
            myUnitId={data.my_unit_id}
            allMembers={allMembers}
            allAgents={data.agents}
            unitChoices={unitChoices}
            actions={actions}
            onSelect={setSelected}
          />
        ) : activeSelected === NO_UNIT ? (
          <div className="mx-auto flex max-w-3xl flex-col gap-4">
            <h2 className="text-heading">{t(($) => $.unassigned)}</h2>
            {forest.unassignedMembers.length > 0 ? (
              <section className="flex flex-col gap-1">
                <h3 className="text-body font-medium">{t(($) => $.members)}</h3>
                {forest.unassignedMembers.map((member) => (
                  <MemberRow
                    key={member.user_id}
                    member={member}
                    canMove={moveChoices.length > 0}
                    canConfigureTwin={data.can_manage}
                    choices={moveChoices}
                    twin={unassignedTwins.get(member.user_id) ?? null}
                    onMove={(unitId) =>
                      void run(
                        () => api.setOrgMemberUnit(member.user_id, unitId),
                        t(($) => $.action_failed),
                      )
                    }
                  />
                ))}
              </section>
            ) : null}
            {unassignedAgents.length > 0 ? (
              <section className="flex flex-col gap-1">
                <h3 className="text-body font-medium">{t(($) => $.agents)}</h3>
                {unassignedAgents.map((agent) => (
                  <AgentCard
                    key={agent.id}
                    agent={agent}
                    canMove={moveChoices.length > 0}
                    choices={moveChoices}
                    onMove={(unitId) =>
                      void run(
                        () => api.setOrgAgentUnit(agent.id, unitId),
                        t(($) => $.action_failed),
                      )
                    }
                  />
                ))}
              </section>
            ) : null}
            {forest.unassignedSquads.length > 0 ? (
              <section className="flex flex-col gap-1">
                <h3 className="text-body font-medium">{t(($) => $.squads)}</h3>
                {forest.unassignedSquads.map((squad) => (
                  <SquadCard
                    key={squad.id}
                    squad={squad}
                    canMove={moveChoices.length > 0}
                    choices={moveChoices}
                    onMove={(unitId) =>
                      void run(
                        () => api.setOrgSquadUnit(squad.id, unitId),
                        t(($) => $.action_failed),
                      )
                    }
                  />
                ))}
              </section>
            ) : null}
          </div>
        ) : !canAnyManage && data.my_unit_id === "" ? (
          <div className="mx-auto max-w-3xl">
            <p className="mb-3 text-caption text-muted-foreground">{t(($) => $.my_org_empty)}</p>
            <p className="text-caption text-muted-foreground">{t(($) => $.pick_unit)}</p>
          </div>
        ) : forest.roots.length > 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.pick_unit)}</p>
        ) : null}
      </main>
    </div>
  );
}

function findNode(
  nodes: ReturnType<typeof buildOrgForest>["roots"],
  unitId: string,
): ReturnType<typeof buildOrgForest>["roots"][number] | null {
  for (const node of nodes) {
    if (node.unit.id === unitId) return node;
    const found = findNode(node.children, unitId);
    if (found) return found;
  }
  return null;
}
