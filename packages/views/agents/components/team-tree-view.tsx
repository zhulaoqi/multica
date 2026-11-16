"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type OrgTree } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Building2, Crown, MessageSquare, Pencil } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { IntroArtCard, IntroArtConnector, IntroArtTag } from "../../layout/feature-intro";
import { AppLink } from "../../navigation";
import { Branch } from "../../org/org-canvas";
import { availabilityConfig } from "../presence";
import { countDepartmentRows, departmentForest, type DepartmentNode } from "../team-tree";
import type { AgentListRow } from "./agents-page";

/**
 * "My team" drawn as the org chart: each department card names its lead and
 * hands off through dashed wires to the agents its members own, then to its
 * sub-departments. Every agent card carries its owner and responsibilities,
 * and offers the lead a direct way to command it or rewrite its duties.
 */
export function TeamTreeView({
  rows,
  orgTree,
  memberName,
}: {
  rows: AgentListRow[];
  orgTree: Pick<OrgTree, "units" | "members">;
  memberName: (userId: string) => string;
}) {
  const forest = departmentForest(rows, (row) => row.agent.owner_id, orgTree);
  return (
    <div className="min-h-0 flex-1 overflow-y-auto" data-testid="team-tree">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-8 p-6">
        {forest.map((node) => (
          <DepartmentBranch key={node.unitId || "unassigned"} node={node} memberName={memberName} />
        ))}
      </div>
    </div>
  );
}

function DepartmentBranch({
  node,
  memberName,
}: {
  node: DepartmentNode<AgentListRow>;
  memberName: (userId: string) => string;
}) {
  return (
    <div className="flex w-full flex-col items-center">
      <DepartmentCard node={node} memberName={memberName} />
      {node.rows.length > 0 ? (
        <>
          <IntroArtConnector />
          <Branch>
            {node.rows.map((row) => (
              <TeamAgentCard key={row.agent.id} row={row} />
            ))}
          </Branch>
        </>
      ) : null}
      {node.children.map((child) => (
        <div key={child.unitId} className="flex w-full flex-col items-center sm:w-[94%]">
          <IntroArtConnector />
          <DepartmentBranch node={child} memberName={memberName} />
        </div>
      ))}
    </div>
  );
}

function DepartmentCard({
  node,
  memberName,
}: {
  node: DepartmentNode<AgentListRow>;
  memberName: (userId: string) => string;
}) {
  const { t } = useT("agents");
  const unassigned = node.unitId === "";
  return (
    <IntroArtCard className="!p-3 sm:w-4/5">
      <div className="flex items-center gap-2">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <Building2 className="size-4" aria-hidden />
        </span>
        <span className="min-w-0 flex-1 truncate text-body font-medium">
          {unassigned ? t(($) => $.page.team_unassigned) : node.name}
        </span>
        <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
          {t(($) => $.team_tree.agent_count, { count: countDepartmentRows(node) })}
        </span>
      </div>
      {unassigned ? null : (
        <div className="mt-2 flex items-center gap-1.5 pl-9">
          <span className="text-muted-foreground">{t(($) => $.team_tree.department_lead)}</span>
          {node.leadUserId ? (
            <span className="flex min-w-0 items-center gap-1 rounded-full bg-muted py-0.5 pr-2 pl-0.5">
              <ActorAvatar actorType="member" actorId={node.leadUserId} size="xs" />
              <span className="truncate font-medium">{memberName(node.leadUserId)}</span>
              <Crown className="size-3 shrink-0 text-warning" aria-hidden />
            </span>
          ) : (
            <span className="text-muted-foreground">{t(($) => $.team_tree.no_lead)}</span>
          )}
        </div>
      )}
    </IntroArtCard>
  );
}

function TeamAgentCard({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const paths = useWorkspacePaths();
  const [editing, setEditing] = useState(false);
  const { agent, presence, owner } = row;
  const working = (presence?.runningCount ?? 0) > 0;
  const duty = agent.intake_can?.trim() || agent.description?.trim() || "";
  const canEdit = row.canManage || agent.viewer_leads_owner === true;

  return (
    <IntroArtCard highlighted={working} className="flex h-full flex-col gap-2 !p-3">
      <div className="flex items-center gap-2">
        <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
        <AppLink
          href={paths.agentDetail(agent.id)}
          className="min-w-0 flex-1 truncate text-body font-medium hover:underline"
        >
          {agent.name}
        </AppLink>
        <PresenceLabel row={row} />
      </div>
      <div className="flex items-center gap-1.5">
        <span className="shrink-0 text-muted-foreground">{t(($) => $.team_tree.owner)}</span>
        {agent.owner_id ? (
          <span className="flex min-w-0 items-center gap-1">
            <ActorAvatar actorType="member" actorId={agent.owner_id} size="xs" />
            <span className="truncate font-medium">{owner?.name ?? agent.owner_id.slice(0, 8)}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </div>
      <div className="flex-1">
        <IntroArtTag className="mb-1">{t(($) => $.team_tree.responsibility)}</IntroArtTag>
        <p className={cn("line-clamp-3 whitespace-pre-line", !duty && "text-muted-foreground")}>
          {duty || t(($) => $.team_tree.no_responsibility)}
        </p>
        {agent.intake_cannot?.trim() ? (
          <p className="mt-1 line-clamp-2 text-muted-foreground">
            {t(($) => $.team_tree.cannot_prefix)}
            {agent.intake_cannot.trim()}
          </p>
        ) : null}
      </div>
      <div className="flex items-center justify-end gap-1">
        {canEdit ? (
          <Button size="xs" variant="ghost" onClick={() => setEditing(true)}>
            <Pencil aria-hidden />
            {t(($) => $.team_tree.edit_responsibility)}
          </Button>
        ) : null}
        <Button
          size="xs"
          variant="outline"
          render={<AppLink href={paths.chatWithAgent(agent.id)} />}
          nativeButton={false}
        >
          <MessageSquare aria-hidden />
          {t(($) => $.team_tree.command)}
        </Button>
      </div>
      {editing ? <ResponsibilityDialog row={row} onClose={() => setEditing(false)} /> : null}
    </IntroArtCard>
  );
}

function PresenceLabel({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const { presence } = row;
  if (!presence) return null;
  if (presence.runningCount > 0) {
    return <span className="shrink-0 font-medium text-brand">{t(($) => $.workload.working)}</span>;
  }
  const visual = availabilityConfig[presence.availability];
  return (
    <span className={cn("shrink-0", visual.textClass)}>
      {t(($) => $.availability[presence.availability])}
    </span>
  );
}

function ResponsibilityDialog({ row, onClose }: { row: AgentListRow; onClose: () => void }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { agent } = row;
  const [can, setCan] = useState(agent.intake_can ?? agent.description ?? "");
  const [cannot, setCannot] = useState(agent.intake_cannot ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await api.patchIntakeAgent(agent.id, {
        name: agent.name,
        can: can.trim(),
        cannot: cannot.trim(),
        runtime_mode: agent.runtime_mode === "cloud" ? "cloud" : "local",
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
        queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) }),
      ]);
      onClose();
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.team_tree.save_failed));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next && !saving) onClose();
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.team_tree.dialog_title, { name: agent.name })}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.team_tree.responsibility)}</span>
            <Textarea value={can} onChange={(event) => setCan(event.target.value)} className="min-h-24 resize-y" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.team_tree.cannot)}</span>
            <Textarea
              value={cannot}
              onChange={(event) => setCannot(event.target.value)}
              className="min-h-14 resize-y"
            />
          </label>
        </div>
        {error ? <p className="text-body text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
            {t(($) => $.team_tree.cancel)}
          </Button>
          <Button size="sm" disabled={saving || !can.trim()} onClick={() => void save()}>
            {t(($) => $.team_tree.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
