"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2 } from "@multica/ui/icons";
import {
  useWorkspacePresenceMap,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useOfficeRoom, type OfficePosition } from "@multica/core/office";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";
import { CollectionPageHeader } from "../layout";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { OfficeCanvas } from "./components/office-canvas";
import { OfficeRoster } from "./components/office-roster";
import type { AgentDeskStatus } from "./scene/characters";
import { buildOfficeLayout, spawnFor } from "./scene/layout";

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

  const handleLocalMove = useCallback(
    (position: OfficePosition) => {
      sendMove(position.x, position.y, position.dir);
    },
    [sendMove],
  );

  const handleAgentClick = useCallback(
    (agentId: string) => push(p.agentDetail(agentId)),
    [push, p],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader icon={Building2} title={t(($) => $.page.title)} />
      {error && (
        <p className="px-4 pt-2 text-caption text-warning">{t(($) => $.page.error)}</p>
      )}
      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <div className="flex min-h-0 flex-1 flex-col gap-2 p-4">
          <OfficeCanvas
            agents={agentViews}
            peers={peerViews}
            localName={localName}
            spawn={spawn}
            onLocalMove={handleLocalMove}
            onAgentClick={handleAgentClick}
          />
          <p className="text-center text-caption text-muted-foreground">
            {joined ? t(($) => $.page.hint) : t(($) => $.page.connecting)}
          </p>
        </div>
        <OfficeRoster
          members={rosterMembers}
          agents={agentViews}
          joined={joined}
          onAgentClick={handleAgentClick}
        />
      </div>
    </div>
  );
}
