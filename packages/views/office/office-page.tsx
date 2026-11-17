"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
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
import type { PlayerEmoteId } from "./game/emotes";
import type { OfficeEventId } from "./game/events";
import { useMyDispatches } from "@multica/core/workbench";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { CollectionPageHeader } from "../layout";
import { isAgentStuck } from "../layout/panels/team-status";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { dispatchPhase } from "../workbench/use-workbench-conversation";
import { AgentInteractPanel } from "./components/agent-interact-panel";
import {
  OfficeCelebration,
  type OfficeCelebrationEvent,
} from "./components/office-celebration";
import { OfficeCanvas, type OfficeCanvasHandle } from "./components/office-canvas";
import { OfficeCoffeePanel } from "./components/office-coffee-panel";
import { OfficeMeetingPanel, type MeetingNote } from "./components/office-meeting-panel";
import { OfficeReceptionPanel } from "./components/office-reception-panel";
import { OfficeRoster } from "./components/office-roster";
import {
  OfficeEmoteWheel,
  OfficePhotoBar,
  OfficeToolbar,
} from "./components/office-toolbar";
import { OfficeTrophyPanel } from "./components/office-trophy-panel";
import { pickCoffeePrompt } from "./game/coffee";
import { departmentRooms } from "./game/departments";
import { playerEmoteForKey } from "./game/emotes";
import {
  agentProgress,
  companyProgress,
  completionsInWindow,
  completionsToday,
  pickMvp,
  weeklyProgress,
} from "./game/progress";
import { seasonFor } from "./game/season";
import type { TrophyStats } from "./game/trophies";
import { OfficeAudio } from "./office-sound";
import type { AgentDeskStatus } from "./scene/characters";
import { isTypingTarget } from "./scene/input";
import { buildOfficeLayout, spawnFor, type RoomId } from "./scene/layout";
import type { DeskTrace, OfficePlace, RoomBadge } from "./scene/office-scene";
import { useAgentActivityText } from "./use-agent-activity";
import { useOpenAgentChat } from "./use-open-agent-chat";

const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];
const TOAST_MS = 4_000;

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
  // Department plaques are a bonus layer: a workspace without an organisation,
  // or a caller that cannot read it, still gets the whole office.
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

  const activeAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at),
    [agents],
  );

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
          return {
            id: agent.id,
            name: agent.name,
            status: deskStatus(detail),
            queuedCount: detail?.queuedCount ?? 0,
            alert: isAgentStuck(detail),
          };
        }),
    [activeAgents, byAgent],
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
  const [placeFocused, setPlaceFocused] = useState<OfficePlace | null>(null);
  const [receptionOpen, setReceptionOpen] = useState(false);
  const [coffeeOpen, setCoffeeOpen] = useState(false);
  const [meetingOpen, setMeetingOpen] = useState(false);
  const [trophiesOpen, setTrophiesOpen] = useState(false);
  const [emotesOpen, setEmotesOpen] = useState(false);
  const [photoMode, setPhotoMode] = useState(false);
  const [photoSaved, setPhotoSaved] = useState(false);
  const [minimapVisible, setMinimapVisible] = useState(true);
  const [soundOn, setSoundOn] = useState(false);
  const [coffeeSeed, setCoffeeSeed] = useState(0);
  const [eventToast, setEventToast] = useState<{ id: OfficeEventId; at: number } | null>(
    null,
  );
  const [celebration, setCelebration] = useState<OfficeCelebrationEvent | null>(null);
  const canvasRef = useRef<OfficeCanvasHandle>(null);
  const audioRef = useRef<OfficeAudio | null>(null);

  const season = useMemo(() => seasonFor(new Date()), []);

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
      else if (phase === "completed" && previous === "in_review") {
        canvasRef.current?.archiveRitual();
        audioRef.current?.cue("sign");
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
        orgTree?.agents ?? [],
        new Map(contributions.map((entry) => [entry.id, entry.completed])),
      ),
    [orgTree, contributions],
  );
  const roomBadges = useMemo<RoomBadge[]>(
    () =>
      departments.map((department) => ({
        room: department.room,
        label: department.name,
        level: department.level,
      })),
    [departments],
  );

  // Desks collect today's traces: a mug after the first run, paper as the
  // day stacks up. Index matches the scene's desk order.
  const deskTraces = useMemo<DeskTrace[]>(
    () =>
      activeAgents.flatMap((agent, deskIndex) => {
        const today = completionsToday(activityByAgent.get(agent.id));
        if (today <= 0) return [];
        return [{ deskIndex, mug: true, papers: Math.min(3, today) }];
      }),
    [activeAgents, activityByAgent],
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
      audioRef.current?.cue("celebrate");
      setCelebration({ kind: "company", level: companyLevel });
    } else if (weekly.met && !previous.weeklyMet) {
      canvasRef.current?.celebrate();
      audioRef.current?.cue("celebrate");
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
    audioRef.current?.cue(event === "cat" ? "meow" : "event");
  }, []);

  const playEmote = useCallback((kind: PlayerEmoteId) => {
    canvasRef.current?.playLocalEmote(kind);
    audioRef.current?.cue("event");
  }, []);

  const toggleSound = useCallback(() => {
    if (audioRef.current?.running) {
      audioRef.current.stop();
      setSoundOn(false);
      return;
    }
    const audio = audioRef.current ?? new OfficeAudio();
    audioRef.current = audio;
    setSoundOn(audio.start());
  }, []);

  // Photo mode is a full-view state; Escape should always be able to leave it.
  useEffect(() => {
    if (!photoMode) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setPhotoMode(false);
      setPhotoSaved(false);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [photoMode]);

  const savePhoto = useCallback(() => {
    const data = canvasRef.current?.capturePhoto();
    if (!data) return;
    const link = document.createElement("a");
    link.href = data;
    link.download = `multica-office-${new Date().toISOString().slice(0, 10)}.png`;
    link.click();
    setPhotoSaved(true);
    window.setTimeout(() => setPhotoSaved(false), 2_500);
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

  const anyPanelOpen =
    !!panelAgentId || receptionOpen || coffeeOpen || meetingOpen || trophiesOpen;
  const canTalk = !!focusedAgent && !panelAgentId;
  const canCheckIn = placeFocused === "reception" && !receptionOpen && !panelAgentId;
  const canCoffee = placeFocused === "coffee" && !coffeeOpen && !panelAgentId;
  const canMeeting = placeFocused === "meeting" && !meetingOpen && !panelAgentId;

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

  // The emote wheel is reachable from the number row so it never needs a
  // click in the middle of a conversation.
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) return;
      const emote = playerEmoteForKey(event.key);
      if (!emote) return;
      event.preventDefault();
      playEmote(emote.id);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [playEmote]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {!photoMode && (
        <CollectionPageHeader icon={Building2} title={t(($) => $.page.title)} />
      )}
      {error && !photoMode && (
        <p className="px-4 pt-2 text-caption text-warning">{t(($) => $.page.error)}</p>
      )}
      <div className={cn("flex min-h-0 flex-1 flex-col lg:flex-row", photoMode && "bg-background")}>
        <div className={cn("flex min-h-0 flex-1 flex-col gap-2", photoMode ? "p-0" : "p-4")}>
          <div className="relative flex min-h-0 flex-1">
            <OfficeCanvas
              ref={canvasRef}
              agents={agentViews}
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
              minimapVisible={minimapVisible}
              engagedAgentId={panelAgent?.id ?? null}
              className={photoMode ? "rounded-none border-0 shadow-none" : undefined}
            />
            {!photoMode && (
              <>
                <OfficeToolbar
                  className="absolute left-3 top-3 z-10"
                  season={season}
                  soundOn={soundOn}
                  minimapOn={minimapVisible}
                  photoMode={photoMode}
                  emotesOpen={emotesOpen}
                  onToggleSound={toggleSound}
                  onToggleMinimap={() => setMinimapVisible((value) => !value)}
                  onTogglePhoto={() => {
                    setPhotoMode(true);
                    setPhotoSaved(false);
                    setEmotesOpen(false);
                    setTrophiesOpen(false);
                  }}
                  onToggleEmotes={() => setEmotesOpen((value) => !value)}
                  onOpenTrophies={() => {
                    setTrophiesOpen(true);
                    setEmotesOpen(false);
                  }}
                />
                {emotesOpen && (
                  <OfficeEmoteWheel
                    className="absolute left-3 top-14 z-10"
                    onEmote={playEmote}
                    onClose={() => setEmotesOpen(false)}
                  />
                )}
                {trophiesOpen && (
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
              </>
            )}
            {celebration && !photoMode && (
              <OfficeCelebration event={celebration} onDismiss={dismissCelebration} />
            )}
            {!anyPanelOpen && !photoMode && focusedAgent && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.interact.hint, {
                    key: "T",
                    name: focusedAgent.name,
                  })}
                </span>
              </div>
            )}
            {!anyPanelOpen && !photoMode && !focusedAgent && placeFocused === "reception" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.reception.hint, { key: "T" })}
                </span>
              </div>
            )}
            {!anyPanelOpen && !photoMode && !focusedAgent && placeFocused === "coffee" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.coffee.hint, { key: "T" })}
                </span>
              </div>
            )}
            {!anyPanelOpen && !photoMode && !focusedAgent && placeFocused === "meeting" && (
              <div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center">
                <span className="rounded-full bg-surface-raised px-3 py-1 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border">
                  {t(($) => $.page.meeting.hint, { key: "T" })}
                </span>
              </div>
            )}
            {receptionOpen && !panelAgent && (
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
            {coffeeOpen && !panelAgent && (
              <OfficeCoffeePanel
                className="absolute bottom-3 left-3"
                prompt={coffeePrompt}
                stuckNames={stuckAgents.map((agent) => agent.name)}
                onAnother={() => setCoffeeSeed((seed) => seed + 1)}
                onClose={closeCoffee}
              />
            )}
            {meetingOpen && !panelAgent && (
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
                canAssign={canAssign(panelAgent)}
                onClose={closePanel}
                onOpenChat={() => openAgentChat(panelAgent.id)}
                onOpenDetail={() => push(p.agentDetail(panelAgent.id))}
                onTaskCreated={handleTaskCreated}
                onMoreOptionsTask={handleMoreOptionsTask}
              />
            )}
            {photoMode && (
              <div className="absolute inset-x-0 bottom-4 z-10 flex justify-center">
                <OfficePhotoBar
                  onSave={savePhoto}
                  onExit={() => {
                    setPhotoMode(false);
                    setPhotoSaved(false);
                  }}
                  saved={photoSaved}
                />
              </div>
            )}
          </div>
          {!photoMode && (
            <p className="text-center text-caption text-muted-foreground">
              {joined ? t(($) => $.page.hint) : t(($) => $.page.connecting)}
            </p>
          )}
        </div>
        {!photoMode && (
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
