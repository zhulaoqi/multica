"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2 } from "@multica/ui/icons";
import {
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useIssueDraftStore } from "@multica/core/issues/stores";
import { useModalStore } from "@multica/core/modals";
import { useOfficeRoom, type OfficePosition } from "@multica/core/office";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, Squad } from "@multica/core/types";
import { useMyDispatches } from "@multica/core/workbench";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { CollectionPageHeader } from "../layout";
import { isAgentStuck } from "../layout/panels/team-status";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { dispatchPhase } from "../workbench/use-workbench-conversation";
import { AgentInteractPanel } from "./components/agent-interact-panel";
import { OfficeCanvas, type OfficeCanvasHandle } from "./components/office-canvas";
import { OfficeRoster } from "./components/office-roster";
import {
  agentProgress,
  companyProgress,
  completionsInWindow,
  pickMvp,
} from "./game/progress";
import type { AgentDeskStatus } from "./scene/characters";
import { isTypingTarget } from "./scene/input";
import { buildOfficeLayout, spawnFor, type RoomId } from "./scene/layout";
import { useAgentActivityText } from "./use-agent-activity";
import { useOpenAgentChat } from "./use-open-agent-chat";

const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];

/** How an agent's live presence reads on its desk. */
export function deskStatus(detail: AgentPresenceDetail | undefined): AgentDeskStatus {
  if (!detail || detail.availability === "offline") return "offline";
  if (detail.workload === "working") return "working";
  if (detail.workload === "queued") return "queued";
  return "idle";
}

export function OfficePage() {
  const { t } = useT("office");
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const wsId = useCurrentWorkspace()?.id ?? "";
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const { data: agents = EMPTY_AGENTS } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId,
  });
  const { byAgent } = useWorkspacePresenceMap(wsId || undefined);
  const { byAgent: activityByAgent } = useWorkspaceActivityMap(wsId || undefined);

  const layout = useMemo(() => buildOfficeLayout(), []);
  const roomNames = useMemo<Record<RoomId, string>>(
    () => ({
      meeting: t(($) => $.page.rooms.meeting),
      kitchen: t(($) => $.page.rooms.kitchen),
      server: t(($) => $.page.rooms.server),
      office: t(($) => $.page.rooms.office),
      lounge: t(($) => $.page.rooms.lounge),
      reception: t(($) => $.page.rooms.reception),
      print: t(($) => $.page.rooms.print),
    }),
    [t],
  );
  const spawn = useMemo<OfficePosition>(
    () => spawnFor(layout, user?.id ?? "guest"),
    [layout, user?.id],
  );

  const { peers, joined, error, sendMove } = useOfficeRoom({
    currentUserId: user?.id ?? null,
    spawn,
  });

  const nameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const member of members) map.set(member.user_id, member.name);
    return map;
  }, [members]);

  const localName = (user?.id ? nameById.get(user.id) : null) ?? user?.name ?? "";

  const peerViews = useMemo(
    () =>
      peers.map((peer) => ({
        ...peer,
        name: nameById.get(peer.userId) ?? t(($) => $.page.roster.member),
      })),
    [peers, nameById, t],
  );

  // Office levels read the same 30-day series as the Agents list, so the
  // roster, the crown, and the company level all agree with the Team panel.
  const contributions = useMemo(
    () =>
      agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => ({
          id: agent.id,
          completed: completionsInWindow(activityByAgent.get(agent.id)),
        })),
    [agents, activityByAgent],
  );
  const mvpId = useMemo(() => pickMvp(contributions), [contributions]);
  const companyLevel = useMemo(
    () => companyProgress(contributions).level,
    [contributions],
  );
  const levelById = useMemo(
    () =>
      new Map(
        contributions.map((entry) => [
          entry.id,
          agentProgress(entry.completed).level,
        ]),
      ),
    [contributions],
  );

  const agentStatuses = useMemo(
    () =>
      agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => {
          const detail = byAgent.get(agent.id);
          return {
            id: agent.id,
            name: agent.name,
            status: deskStatus(detail),
            queuedCount: detail?.queuedCount ?? 0,
            alert: isAgentStuck(detail),
          };
        }),
    [agents, byAgent],
  );
  const activityById = useAgentActivityText(wsId, agentStatuses);
  const agentViews = useMemo(
    () =>
      agentStatuses.map(({ id, name, status, alert }) => ({
        id,
        name,
        status,
        alert,
        activity: activityById.get(id) ?? null,
      })),
    [agentStatuses, activityById],
  );
  const rosterAgents = useMemo(
    () =>
      agentViews.map((agent) => ({
        ...agent,
        level: levelById.get(agent.id) ?? 1,
        isMvp: agent.id === mvpId,
      })),
    [agentViews, levelById, mvpId],
  );

  const rosterMembers = useMemo(() => {
    const self = user
      ? [{ userId: user.id, name: localName, isSelf: true }]
      : [];
    const others = peerViews.map((peer) => ({
      userId: peer.userId,
      name: peer.name,
      isSelf: false,
    }));
    return [...self, ...others];
  }, [user, localName, peerViews]);

  const [focusedAgentId, setFocusedAgentId] = useState<string | null>(null);
  const [panelAgentId, setPanelAgentId] = useState<string | null>(null);
  const canvasRef = useRef<OfficeCanvasHandle>(null);

  // The office reacts to real work: a delivered dispatch sends its squad
  // leader to the front desk with a paper, a failed one raises an alert. Only
  // changes after the first load animate, so opening the page does not replay
  // yesterday's history.
  const { dispatches } = useMyDispatches(wsId);
  const { data: squads = EMPTY_SQUADS } = useQuery({
    ...squadListOptions(wsId),
    enabled: !!wsId,
  });
  const seenPhases = useRef(new Map<string, ReturnType<typeof dispatchPhase>>());
  useEffect(() => {
    const leaderBySquad = new Map(
      squads
        .filter((squad) => !squad.archived_at)
        .map((squad) => [squad.id, squad.leader_id]),
    );
    const seen = seenPhases.current;
    for (const item of dispatches) {
      const phase = dispatchPhase(item, undefined);
      const previous = seen.get(item.issue.id);
      seen.set(item.issue.id, phase);
      if (previous === undefined || previous === phase) continue;
      const squadId =
        item.issue.assignee_type === "squad" ? item.issue.assignee_id : null;
      const leaderId = squadId ? leaderBySquad.get(squadId) : null;
      if (!leaderId) continue;
      if (phase === "in_review") canvasRef.current?.deliverToReception(leaderId);
      else if (phase === "failed") canvasRef.current?.playEmote(leaderId, "alert");
    }
  }, [dispatches, squads]);

  const focusedAgent = useMemo(
    () => agents.find((agent) => agent.id === focusedAgentId && !agent.archived_at) ?? null,
    [agents, focusedAgentId],
  );
  const panelAgent = useMemo(
    () => agents.find((agent) => agent.id === panelAgentId && !agent.archived_at) ?? null,
    [agents, panelAgentId],
  );

  // The agent may be archived or removed while its panel is open.
  useEffect(() => {
    if (!panelAgentId) return;
    if (agents.length > 0 && !panelAgent) setPanelAgentId(null);
  }, [agents.length, panelAgent, panelAgentId]);

  const role = members.find((member) => member.user_id === user?.id)?.role;
  const canAssign = useCallback(
    (agent: Agent) => canAssignAgent(agent, user?.id, role),
    [user?.id, role],
  );

  const openAgentChat = useOpenAgentChat(wsId);

  const handleLocalMove = useCallback(
    (position: OfficePosition) => {
      sendMove(position.x, position.y, position.dir);
    },
    [sendMove],
  );

  const handleAgentActivate = useCallback((agentId: string) => {
    setPanelAgentId(agentId);
  }, []);

  const handleFocusAgentChange = useCallback((agentId: string | null) => {
    setFocusedAgentId(agentId);
  }, []);

  // Walking over to an agent from the roster keeps the avatar in sync with
  // what the panel is showing.
  const handleAgentSelect = useCallback((agentId: string) => {
    setPanelAgentId(agentId);
    canvasRef.current?.visitAgent(agentId);
  }, []);

  const closePanel = useCallback(() => setPanelAgentId(null), []);

  const handleTaskCreated = useCallback(() => {
    if (panelAgent) canvasRef.current?.playEmote(panelAgent.id, "check");
  }, [panelAgent]);

  const handleMoreOptionsTask = useCallback(
    (prompt: string) => {
      if (!panelAgent) return;
      const drafts = useIssueDraftStore.getState();
      drafts.setAgent({
        prompt,
        actorType: "agent",
        actorId: panelAgent.id,
      });
      drafts.setActiveMode("agent");
      useModalStore.getState().open("quick-create-issue", {
        agent_id: panelAgent.id,
      });
    },
    [panelAgent],
  );

  // T next to an agent is the keyboard route into the same walk-up flow.
  useEffect(() => {
    if (!focusedAgent || panelAgentId) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) return;
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      if (key !== "t") return;
      event.preventDefault();
      canvasRef.current?.visitAgent(focusedAgent.id);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [focusedAgent, panelAgentId]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader icon={Building2} title={t(($) => $.page.title)} />
      {error && (
        <p className="px-4 pt-2 text-caption text-warning">{t(($) => $.page.error)}</p>
      )}
      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <div className="flex min-h-0 flex-1 flex-col gap-2 p-4">
          <div className="relative flex min-h-0 flex-1">
            <OfficeCanvas
              ref={canvasRef}
              agents={agentViews}
              peers={peerViews}
              localName={localName}
              roomNames={roomNames}
              companyName={t(($) => $.page.company_name)}
              companyLevel={companyLevel}
              spawn={spawn}
              onLocalMove={handleLocalMove}
              onAgentActivate={handleAgentActivate}
              onFocusAgentChange={handleFocusAgentChange}
              engagedAgentId={panelAgent?.id ?? null}
            />
            {focusedAgent && !panelAgent && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.interact.hint, {
                    key: "T",
                    name: focusedAgent.name,
                  })}
                </span>
              </div>
            )}
            {panelAgent && (
              <AgentInteractPanel
                className="absolute bottom-3 left-3"
                agent={panelAgent}
                status={deskStatus(byAgent.get(panelAgent.id))}
                canAssign={canAssign(panelAgent)}
                onClose={closePanel}
                onOpenChat={() => openAgentChat(panelAgent.id)}
                onOpenDetail={() => push(p.agentDetail(panelAgent.id))}
                onTaskCreated={handleTaskCreated}
                onMoreOptionsTask={handleMoreOptionsTask}
              />
            )}
          </div>
          <p className="text-center text-caption text-muted-foreground">
            {joined ? t(($) => $.page.hint) : t(($) => $.page.connecting)}
          </p>
        </div>
        <OfficeRoster
          members={rosterMembers}
          agents={rosterAgents}
          companyLevel={companyLevel}
          joined={joined}
          onAgentSelect={handleAgentSelect}
        />
      </div>
    </div>
  );
}
