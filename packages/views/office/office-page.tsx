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
  orgTreeOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, Squad } from "@multica/core/types";
import type { OfficeEventId } from "./game/events";
import { teamUserIdsFor } from "../agents/team-scope";
import { unitPaths } from "../org/org-tree";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { CollectionPageHeader } from "../layout";
import { isAgentStuck } from "../layout/panels/team-status";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { useMyDispatchScope } from "../workbench/use-my-dispatch-scope";
import { dispatchPhase } from "../workbench/use-workbench-conversation";
import { AgentInteractPanel } from "./components/agent-interact-panel";
import { OfficeBrief } from "./components/office-brief";
import {
  OfficeCelebration,
  type OfficeCelebrationEvent,
} from "./components/office-celebration";
import { OfficeCanvas, type OfficeCanvasHandle } from "./components/office-canvas";
import { OfficeCoffeePanel } from "./components/office-coffee-panel";
import { OfficeMeetingPanel, type MeetingNote } from "./components/office-meeting-panel";
import { OfficeReceptionPanel } from "./components/office-reception-panel";
import { OfficeRoster } from "./components/office-roster";
import { OfficeToolbar } from "./components/office-toolbar";
import { OfficeTrophyPanel } from "./components/office-trophy-panel";
import { pickCoffeePrompt } from "./game/coffee";
import { departmentRooms } from "./game/departments";
import {
  agentProgress,
  companyProgress,
  completionsInWindow,
  completionsToday,
  pickMvp,
  weeklyProgress,
} from "./game/progress";
import { seasonFor } from "./game/season";
import { assignDeskZones } from "./game/zones";
import type { TrophyStats } from "./game/trophies";
import { OfficeAudio } from "./office-sound";
import type { AgentDeskStatus } from "./scene/characters";
import { isTypingTarget } from "./scene/input";
import {
  buildOfficeLayout,
  OFFICE_DESK_COUNT,
  spawnFor,
  type RoomId,
} from "./scene/layout";
import type { DeskTrace, OfficePlace, RoomBadge } from "./scene/office-scene";
import { useAgentActivityText } from "./use-agent-activity";
import { useMyOfficeActivityText } from "./use-my-office-activity";
import { useOpenAgentChat } from "./use-open-agent-chat";

const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];
const TOAST_MS = 4_000;
const SOUND_STORAGE_KEY = "multica.office.sound";

/** How an agent's live presence reads on its desk. */
export function deskStatus(detail: AgentPresenceDetail | undefined): AgentDeskStatus {
  if (!detail || detail.availability === "offline") return "offline";
  if (detail.workload === "working") return "working";
  if (detail.workload === "queued") return "queued";
  return "idle";
}

export function OfficePage({
  variant = "workspace",
}: {
  variant?: "workspace" | "home";
} = {}) {
  const { t } = useT("office");
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const wsId = useCurrentWorkspace()?.id ?? "";
  const user = useAuthStore((s) => s.user);
  // The C-end office is a window onto the caller's own work: only the agents
  // on their dispatches, and bubbles naming their tasks. The admin office
  // shows the whole company.
  const home = variant === "home";
  const scope = useMyDispatchScope(wsId);
  const dispatches = scope.dispatches;

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const { data: agents = EMPTY_AGENTS } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId,
  });
  // The org tree drives both layers: the workspace office uses it for room
  // plaques, and the C-end uses it to resolve the lead's own team and the
  // department each agent desks in. A workspace without an organisation, or a
  // caller that cannot read it, still gets the whole office.
  const { data: orgTree } = useQuery({
    ...orgTreeOptions(wsId),
    enabled: !!wsId,
    retry: false,
  });
  const { byAgent } = useWorkspacePresenceMap(wsId || undefined);
  const { byAgent: activityByAgent, loading: activityLoading } = useWorkspaceActivityMap(
    wsId || undefined,
  );

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
    // The org tree can name owners outside the member list (e.g. a lead
    // viewing a colleague who has not joined the project); never let an
    // empty name win over a real one.
    for (const member of orgTree?.members ?? []) {
      if (member.name) map.set(member.user_id, member.name);
    }
    return map;
  }, [members, orgTree]);
  const unitPathById = useMemo(
    () => unitPaths(orgTree?.units ?? []),
    [orgTree],
  );

  const localName = (user?.id ? nameById.get(user.id) : null) ?? user?.name ?? "";

  const peerViews = useMemo(
    () =>
      peers.map((peer) => ({
        ...peer,
        name: nameById.get(peer.userId) ?? t(($) => $.page.roster.member),
      })),
    [peers, nameById, t],
  );

  const role = members.find((member) => member.user_id === user?.id)?.role;
  const isWorkspaceAdmin = role === "owner" || role === "admin";
  // A department lead's team: members of every unit they manage, plus
  // themselves. Null for everyone who leads nothing.
  const teamUserIds = useMemo(
    () => teamUserIdsFor(orgTree, user?.id),
    [orgTree, user?.id],
  );

  const activeAgents = useMemo(() => {
    const live = agents.filter((agent) => !agent.archived_at);
    if (!home) return live;
    if (isWorkspaceAdmin) return live;
    // Leads see their whole team, not just the agents on their own dispatches.
    if (teamUserIds) {
      return live.filter(
        (agent) => !!agent.owner_id && teamUserIds.has(agent.owner_id),
      );
    }
    return live.filter(
      (agent) => scope.agentIds.has(agent.id) || scope.workingAgentIds.has(agent.id),
    );
  }, [
    agents,
    home,
    isWorkspaceAdmin,
    teamUserIds,
    scope.agentIds,
    scope.workingAgentIds,
  ]);

  // Office levels read the same 30-day series as the Agents list, so the
  // roster, the crown, and the company level all agree with the Team panel.
  const contributions = useMemo(
    () =>
      activeAgents.map((agent) => ({
        id: agent.id,
        completed: completionsInWindow(activityByAgent.get(agent.id)),
      })),
    [activeAgents, activityByAgent],
  );
  const completedById = useMemo(
    () => new Map(contributions.map((entry) => [entry.id, entry.completed])),
    [contributions],
  );
  const unitByAgent = useMemo(() => {
    const map = new Map<string, string>();
    for (const agent of orgTree?.agents ?? []) {
      if (agent.unit_id) map.set(agent.id, agent.unit_id);
    }
    return map;
  }, [orgTree]);
  // Departments get contiguous desk clusters, so AIHUB and BI中台 do not sit
  // interleaved. The assignment is deterministic and independent of status.
  const zoneAssignment = useMemo(
    () =>
      assignDeskZones(
        activeAgents,
        unitByAgent,
        orgTree?.units ?? [],
        completedById,
      ),
    [activeAgents, unitByAgent, orgTree, completedById],
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

  // The front desk reports the same week the roster levels read: trailing
  // seven daily buckets, summed across every active agent.
  const weekly = useMemo(
    () =>
      weeklyProgress(
        activeAgents.map((agent) => activityByAgent.get(agent.id)),
        activeAgents.length,
      ),
    [activeAgents, activityByAgent],
  );
  const todayCompleted = useMemo(
    () =>
      activeAgents.reduce(
        (sum, agent) => sum + completionsToday(activityByAgent.get(agent.id)),
        0,
      ),
    [activeAgents, activityByAgent],
  );
  const mvpName = useMemo(() => {
    if (!mvpId) return null;
    return activeAgents.find((agent) => agent.id === mvpId)?.name ?? null;
  }, [mvpId, activeAgents]);

  const agentStatuses = useMemo(
    () =>
      activeAgents
        .map((agent) => {
          const detail = byAgent.get(agent.id);
          const unitId = unitByAgent.get(agent.id);
          return {
            id: agent.id,
            name: agent.name,
            ownerName: agent.owner_id
              ? nameById.get(agent.owner_id) ?? null
              : null,
            unitPath:
              unitId && unitPathById.has(unitId)
                ? unitPathById.get(unitId) ?? null
                : null,
            status: deskStatus(detail),
            queuedCount: detail?.queuedCount ?? 0,
            alert: isAgentStuck(detail),
          };
        }),
    [activeAgents, byAgent, nameById, unitByAgent, unitPathById],
  );
  const brief = useMemo(() => {
    let busiest: string | null = null;
    let best = 0;
    for (const agent of activeAgents) {
      const done = completionsToday(activityByAgent.get(agent.id));
      if (done > best) {
        best = done;
        busiest = agent.name;
      }
    }
    return {
      completed: todayCompleted,
      running: agentStatuses.filter((agent) => agent.status === "working").length,
      busiest,
    };
  }, [activeAgents, activityByAgent, agentStatuses, todayCompleted]);
  const workspaceActivityById = useAgentActivityText(wsId, agentStatuses);
  const myLabels = useMemo(() => {
    const labels = new Map<string, string>();
    for (const item of dispatches) {
      labels.set(item.issue.id, `${item.issue.identifier} ${item.issue.title}`);
    }
    for (const children of scope.childrenByRoot.values()) {
      for (const child of children) {
        labels.set(child.id, `${child.identifier} ${child.title}`);
      }
    }
    return labels;
  }, [dispatches, scope.childrenByRoot]);
  const myActivityById = useMyOfficeActivityText({
    agents: agentStatuses,
    tasksByIssue: scope.tasksByIssue,
    labels: myLabels,
  });
  // The C-end prefers the caller's own dispatch wording, then falls back to
  // the workspace-wide activity so team agents keep a meaningful bubble.
  const activityById = useMemo(() => {
    if (!home) return workspaceActivityById;
    const merged = new Map(workspaceActivityById);
    for (const [id, text] of myActivityById) merged.set(id, text);
    return merged;
  }, [home, myActivityById, workspaceActivityById]);
  const agentViews = useMemo(
    () =>
      agentStatuses.map(({ id, name, ownerName, unitPath, status, alert }) => ({
        id,
        name,
        ownerName,
        unitPath,
        status,
        alert,
        activity: activityById.get(id) ?? null,
      })),
    [agentStatuses, activityById],
  );
  // The canvas seats agents by their department zone; the roster keeps the
  // original ordering.
  const officeAgentViews = useMemo(() => {
    const byId = new Map(agentViews.map((view) => [view.id, view]));
    return zoneAssignment.seats.flatMap(({ id, home: seat }) => {
      const view = byId.get(id);
      return view ? [{ ...view, home: seat }] : [];
    });
  }, [agentViews, zoneAssignment]);
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
  const [placeFocused, setPlaceFocused] = useState<OfficePlace | null>(null);
  const [receptionOpen, setReceptionOpen] = useState(false);
  const [coffeeOpen, setCoffeeOpen] = useState(false);
  const [meetingOpen, setMeetingOpen] = useState(false);
  const [trophiesOpen, setTrophiesOpen] = useState(false);
  const [minimapVisible, setMinimapVisible] = useState(true);
  const [soundOn, setSoundOn] = useState(false);
  const [coffeeSeed, setCoffeeSeed] = useState(0);
  const [eventToast, setEventToast] = useState<{ id: OfficeEventId; at: number } | null>(
    null,
  );
  const [celebration, setCelebration] = useState<OfficeCelebrationEvent | null>(null);
  const canvasRef = useRef<OfficeCanvasHandle>(null);
  const audioRef = useRef<OfficeAudio | null>(null);
  const soundWantedRef = useRef(false);

  const season = useMemo(() => seasonFor(new Date()), []);

  // The office reacts to real work: a delivered dispatch sends its squad
  // leader to the front desk with a paper, a failed one raises an alert. Only
  // changes after the first load animate, so opening the page does not replay
  // yesterday's history.
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
      else if (phase === "failed") {
        canvasRef.current?.playEmote(leaderId, "alert");
        audioRef.current?.cue("alert");
      } else if (phase === "completed" && previous === "in_review") {
        canvasRef.current?.archiveRitual();
        audioRef.current?.cue("complete");
      }
    }
  }, [dispatches, squads]);

  const waitingReview = useMemo(
    () => dispatches.filter((item) => dispatchPhase(item, undefined) === "in_review"),
    [dispatches],
  );
  const stuckAgents = useMemo(
    () =>
      agentStatuses
        .filter((agent) => agent.alert)
        .map(({ id, name }) => ({ id, name })),
    [agentStatuses],
  );

  const archivedCount = useMemo(
    () =>
      dispatches.filter((item) => dispatchPhase(item, undefined) === "completed")
        .length,
    [dispatches],
  );
  const meetingNotes = useMemo<MeetingNote[]>(
    () =>
      dispatches
        .map((item) => ({
          item,
          phase: dispatchPhase(item, undefined) as string,
        }))
        .filter(
          ({ phase }) =>
            phase === "queued" ||
            phase === "decomposing" ||
            phase === "progress" ||
            phase === "in_review",
        )
        .slice(0, 6)
        .map(({ item, phase }) => ({
          id: item.issue.id,
          title: item.issue.title || item.issue.identifier,
          phase,
        })),
    [dispatches],
  );

  // Each agent's room plaque reads its department's work over the same
  // 30-day window as everything else. Org data is optional.
  const departments = useMemo(
    () =>
      departmentRooms(
        orgTree?.units ?? [],
        // Use the same visible roster as the desk planner, so a department
        // cannot show two different levels on the floor and on its wall.
        activeAgents.flatMap((agent) => {
          const unitId = unitByAgent.get(agent.id);
          return unitId ? [{ id: agent.id, unit_id: unitId }] : [];
        }),
        completedById,
      ),
    [orgTree, activeAgents, unitByAgent, completedById],
  );
  // Room plaques describe the company, so the C-end keeps them off; it shows
  // the desk-zone plaques instead.
  const roomBadges = useMemo<RoomBadge[]>(
    () =>
      home
        ? []
        : departments.map((department) => ({
            room: department.room,
            label: department.name,
            level: department.level,
          })),
    [home, departments],
  );

  // Desks collect today's traces: a mug after the first run, paper as the
  // day stacks up. The slot matches the agent's zoned seat.
  const deskTraces = useMemo<DeskTrace[]>(
    () =>
      zoneAssignment.seats.flatMap(({ id, home: seat }) => {
        if (seat >= OFFICE_DESK_COUNT) return [];
        const today = completionsToday(activityByAgent.get(id));
        if (today <= 0) return [];
        return [{ deskIndex: seat, mug: true, papers: Math.min(3, today) }];
      }),
    [zoneAssignment, activityByAgent],
  );

  const trophyStats = useMemo<TrophyStats>(
    () => ({
      archived: archivedCount,
      teamSize: activeAgents.length,
      companyLevel,
      totalCompleted: contributions.reduce((sum, entry) => sum + entry.completed, 0),
      weeklyMet: weekly.met,
      mvpLevel: mvpId ? (levelById.get(mvpId) ?? 1) : 1,
      departments: departments.length,
    }),
    [
      archivedCount,
      activeAgents.length,
      companyLevel,
      contributions,
      weekly.met,
      mvpId,
      levelById,
      departments.length,
    ],
  );

  const coffeePrompt = useMemo(
    () => pickCoffeePrompt(`${wsId}:${coffeeSeed}`),
    [wsId, coffeeSeed],
  );

  // Levels and the shared goal are milestones, not state: announce them when
  // they move, once the activity series has loaded so opening the page does
  // not replay an old one.
  const seenMilestones = useRef<{ level: number; weeklyMet: boolean } | null>(null);
  useEffect(() => {
    if (activityLoading) return;
    const previous = seenMilestones.current;
    const next = { level: companyLevel, weeklyMet: weekly.met };
    if (!previous) {
      seenMilestones.current = next;
      return;
    }
    seenMilestones.current = next;
    if (companyLevel > previous.level) {
      canvasRef.current?.celebrate();
      audioRef.current?.cue("level");
      setCelebration({ kind: "company", level: companyLevel });
    } else if (weekly.met && !previous.weeklyMet) {
      canvasRef.current?.celebrate();
      audioRef.current?.cue("level");
      setCelebration({ kind: "weekly", count: weekly.completed });
    }
  }, [activityLoading, companyLevel, weekly.met, weekly.completed]);

  useEffect(() => {
    if (!eventToast) return;
    const timer = window.setTimeout(() => setEventToast(null), TOAST_MS);
    return () => window.clearTimeout(timer);
  }, [eventToast]);

  useEffect(() => () => audioRef.current?.stop(), []);

  const dismissCelebration = useCallback(() => setCelebration(null), []);
  const closeReception = useCallback(() => setReceptionOpen(false), []);
  const closeCoffee = useCallback(() => setCoffeeOpen(false), []);
  const closeMeeting = useCallback(() => setMeetingOpen(false), []);
  const handlePlaceFocusChange = useCallback((place: OfficePlace | null) => {
    setPlaceFocused(place);
  }, []);

  const handleRandomEvent = useCallback((event: OfficeEventId) => {
    setEventToast({ id: event, at: Date.now() });
  }, []);

  const toggleSound = useCallback(() => {
    const persist = (on: boolean) => {
      try {
        window.localStorage.setItem(SOUND_STORAGE_KEY, on ? "on" : "off");
      } catch {
        // Private mode or blocked storage: sound still works for this visit.
      }
    };
    if (audioRef.current?.running) {
      audioRef.current.stop();
      soundWantedRef.current = false;
      setSoundOn(false);
      persist(false);
      return;
    }
    const audio = audioRef.current ?? new OfficeAudio();
    audioRef.current = audio;
    const started = audio.start();
    setSoundOn(started);
    if (started) {
      soundWantedRef.current = false;
      persist(true);
    }
  }, []);

  // A saved "on" preference still needs a gesture before the browser will
  // actually play; arm a one-shot listener instead of failing silently.
  useEffect(() => {
    let stored = false;
    try {
      stored = window.localStorage.getItem(SOUND_STORAGE_KEY) === "on";
    } catch {
      return;
    }
    if (!stored) return;
    soundWantedRef.current = true;

    const begin = (event: Event) => {
      if (!soundWantedRef.current) return;
      // The toolbar button's own click must decide on/off; starting the audio
      // on pointerdown would make the first click turn the sound back off.
      const target = event.target;
      if (
        target instanceof Element &&
        target.closest('[data-testid="office-toolbar-sound"]')
      ) {
        return;
      }
      const audio = audioRef.current ?? new OfficeAudio();
      audioRef.current = audio;
      const started = audio.start();
      setSoundOn(started);
      if (!started) return;
      soundWantedRef.current = false;
      window.removeEventListener("pointerdown", begin);
      window.removeEventListener("keydown", begin);
    };
    window.addEventListener("pointerdown", begin);
    window.addEventListener("keydown", begin);
    return () => {
      window.removeEventListener("pointerdown", begin);
      window.removeEventListener("keydown", begin);
    };
  }, []);

  // Background tabs suspend audio; the pad must not pile up notes and burst
  // when the user comes back.
  useEffect(() => {
    const onVisibility = () => {
      audioRef.current?.setHidden(document.visibilityState === "hidden");
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);

  const handleAccepted = useCallback(() => {
    canvasRef.current?.archiveRitual();
    audioRef.current?.cue("sign");
  }, []);

  // The agent panel and the room panels share the same corner.
  useEffect(() => {
    if (panelAgentId) {
      setReceptionOpen(false);
      setCoffeeOpen(false);
      setMeetingOpen(false);
    }
  }, [panelAgentId]);

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

  const anyPanelOpen =
    !!panelAgentId || receptionOpen || coffeeOpen || meetingOpen || trophiesOpen;
  const canTalk = !!focusedAgent && !panelAgentId;
  const canCheckIn =
    !home && placeFocused === "reception" && !receptionOpen && !panelAgentId;
  const canCoffee = !home && placeFocused === "coffee" && !coffeeOpen && !panelAgentId;
  const canMeeting = !home && placeFocused === "meeting" && !meetingOpen && !panelAgentId;

  // T is the walk-up key: talk to the agent in front of you, or check in at
  // the front desk, grab a coffee, or look at the board. A person always wins
  // over the place behind them.
  useEffect(() => {
    if (!canTalk && !canCheckIn && !canCoffee && !canMeeting) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) return;
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      if (key !== "t") return;
      event.preventDefault();
      if (canTalk && focusedAgent) canvasRef.current?.visitAgent(focusedAgent.id);
      else if (canCheckIn) {
        setCoffeeOpen(false);
        setMeetingOpen(false);
        setReceptionOpen(true);
      } else if (canCoffee) {
        setReceptionOpen(false);
        setMeetingOpen(false);
        setCoffeeOpen(true);
      } else if (canMeeting) {
        setReceptionOpen(false);
        setCoffeeOpen(false);
        setMeetingOpen(true);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [canTalk, canCheckIn, canCoffee, canMeeting, focusedAgent]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {!home && (
        <CollectionPageHeader icon={Building2} title={t(($) => $.page.title)} />
      )}
      {error && (
        <p className="px-4 pt-2 text-caption text-warning">{t(($) => $.page.error)}</p>
      )}
      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <div className="flex min-h-0 flex-1 flex-col gap-2 p-4">
          <div className="relative flex min-h-0 flex-1">
            <OfficeCanvas
              ref={canvasRef}
              agents={officeAgentViews}
              peers={peerViews}
              localName={localName}
              roomNames={roomNames}
              companyName={t(($) => $.page.company_name)}
              companyLevel={companyLevel}
              season={season}
              spawn={spawn}
              onLocalMove={handleLocalMove}
              onAgentActivate={handleAgentActivate}
              onFocusAgentChange={handleFocusAgentChange}
              onPlaceFocusChange={handlePlaceFocusChange}
              onRandomEvent={handleRandomEvent}
              receptionPapers={waitingReview.length}
              deskTraces={deskTraces}
              roomBadges={roomBadges}
              deskZones={zoneAssignment.zones}
              minimapVisible={minimapVisible}
              engagedAgentId={panelAgent?.id ?? null}
            />
            {home && !scope.isLoading && activeAgents.length === 0 && (
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
                <p className="max-w-xs rounded-lg bg-surface-raised/95 px-4 py-3 text-center text-body text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.home_empty)}
                </p>
              </div>
            )}
            <OfficeToolbar
              className="absolute left-3 top-3 z-10"
              season={season}
              soundOn={soundOn}
              minimapOn={minimapVisible}
              onToggleSound={toggleSound}
              onToggleMinimap={() => setMinimapVisible((value) => !value)}
              onOpenTrophies={() => setTrophiesOpen(true)}
              showTrophies={!home}
            />
            {trophiesOpen && !home && (
              <OfficeTrophyPanel
                className="absolute right-3 top-3 z-10"
                stats={trophyStats}
                departments={departments}
                season={season}
                onClose={() => setTrophiesOpen(false)}
              />
            )}
            {eventToast && (
              <div className="pointer-events-none absolute inset-x-0 top-3 z-10 flex justify-center">
                <span className="rounded-full bg-surface-raised/95 px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.events[eventToast.id])}
                </span>
              </div>
            )}
            {home && (
              <OfficeBrief
                className="absolute right-3 top-3 z-10 max-w-[60%] justify-end"
                completed={brief.completed}
                running={brief.running}
                busiest={brief.busiest}
              />
            )}
            {celebration && (
              <OfficeCelebration event={celebration} onDismiss={dismissCelebration} />
            )}
            {!anyPanelOpen && focusedAgent && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.interact.hint, {
                    key: "T",
                    name: focusedAgent.name,
                  })}
                </span>
              </div>
            )}
            {!home && !anyPanelOpen && !focusedAgent && placeFocused === "reception" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.reception.hint, { key: "T" })}
                </span>
              </div>
            )}
            {!home && !anyPanelOpen && !focusedAgent && placeFocused === "coffee" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.coffee.hint, { key: "T" })}
                </span>
              </div>
            )}
            {!home && !anyPanelOpen && !focusedAgent && placeFocused === "meeting" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.meeting.hint, { key: "T" })}
                </span>
              </div>
            )}
            {!home && receptionOpen && !panelAgent && (
              <OfficeReceptionPanel
                className="absolute bottom-3 left-3"
                wsId={wsId}
                todayCompleted={todayCompleted}
                weekly={weekly}
                companyLevel={companyLevel}
                mvpName={mvpName}
                waiting={waitingReview}
                stuck={stuckAgents}
                onClose={closeReception}
                onAccepted={handleAccepted}
              />
            )}
            {!home && coffeeOpen && !panelAgent && (
              <OfficeCoffeePanel
                className="absolute bottom-3 left-3"
                prompt={coffeePrompt}
                stuckNames={stuckAgents.map((agent) => agent.name)}
                onAnother={() => setCoffeeSeed((seed) => seed + 1)}
                onClose={closeCoffee}
              />
            )}
            {!home && meetingOpen && !panelAgent && (
              <OfficeMeetingPanel
                className="absolute bottom-3 left-3"
                notes={meetingNotes}
                onClose={closeMeeting}
              />
            )}
            {panelAgent && (
              <AgentInteractPanel
                className="absolute bottom-3 left-3"
                agent={panelAgent}
                status={deskStatus(byAgent.get(panelAgent.id))}
                ownerName={
                  panelAgent.owner_id
                    ? nameById.get(panelAgent.owner_id) ?? null
                    : null
                }
                unitPath={(() => {
                  const unitId = unitByAgent.get(panelAgent.id);
                  return unitId ? unitPathById.get(unitId) ?? null : null;
                })()}
                agentTodayCompleted={completionsToday(
                  activityByAgent.get(panelAgent.id),
                )}
                canAssign={canAssign(panelAgent)}
                onClose={closePanel}
                onOpenChat={() => openAgentChat(panelAgent.id)}
                onOpenDetail={() => push(p.agentDetail(panelAgent.id))}
                onTaskCreated={handleTaskCreated}
                onMoreOptionsTask={handleMoreOptionsTask}
                showActions={!home}
                activity={activityById.get(panelAgent.id) ?? null}
              />
            )}
          </div>
          <p className="text-center text-caption text-muted-foreground">
            {joined ? t(($) => $.page.hint) : t(($) => $.page.connecting)}
          </p>
        </div>
        {!home && (
          <OfficeRoster
            members={rosterMembers}
            agents={rosterAgents}
            companyLevel={companyLevel}
            joined={joined}
            onAgentSelect={handleAgentSelect}
          />
        )}
      </div>
    </div>
  );
}
