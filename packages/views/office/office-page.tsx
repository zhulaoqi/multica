"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2 } from "@multica/ui/icons";
import {
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
} from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { CollectionPageHeader } from "../layout";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { AgentInteractPanel } from "./components/agent-interact-panel";
import { OfficeCanvas, type OfficeCanvasHandle } from "./components/office-canvas";
import { OfficeRoster } from "./components/office-roster";
import type { AgentDeskStatus } from "./scene/characters";
import { isTypingTarget } from "./scene/input";
import { buildOfficeLayout, spawnFor, type RoomId } from "./scene/layout";
import { useOpenAgentChat } from "./use-open-agent-chat";

const EMPTY_AGENTS: Agent[] = [];

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

  const agentViews = useMemo(
    () =>
      agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => ({
          id: agent.id,
          name: agent.name,
          status: deskStatus(byAgent.get(agent.id)),
        })),
    [agents, byAgent],
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
          agents={agentViews}
          joined={joined}
          onAgentSelect={handleAgentSelect}
        />
      </div>
    </div>
  );
}
