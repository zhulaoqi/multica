"use client";

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { cn } from "@multica/ui/lib/utils";
import type { OfficePosition } from "@multica/core/office";
import type { BubbleKind } from "../scene/characters";
import type { RoomId } from "../scene/layout";
import {
  OfficeScene,
  type DeskTrace,
  type OfficeAgentView,
  type OfficePeerView,
  type OfficePlace,
  type RoomBadge,
} from "../scene/office-scene";
import type { OfficeEventId } from "../game/events";
import type { DeskZone } from "../game/zones";
import type { PlayerEmoteId } from "../game/emotes";
import type { SeasonTheme } from "../game/season";

export interface OfficeCanvasProps {
  agents: OfficeAgentView[];
  peers: OfficePeerView[];
  localName: string;
  roomNames: Record<RoomId, string>;
  companyName: string;
  /** Company level; higher levels unlock office decorations. */
  companyLevel: number;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  /** The player reached an agent (walked over or clicked it). */
  onAgentActivate: (agentId: string) => void;
  /** Nearest agent changed, for the interact hint. */
  onFocusAgentChange: (agentId: string | null) => void;
  /** The player reached a walk-up place, for the matching hint. */
  onPlaceFocusChange?: (place: OfficePlace | null) => void;
  /** A cosmetic office beat happened. */
  onRandomEvent?: (event: OfficeEventId) => void;
  /** Ambient season for the drifting particles. */
  season: SeasonTheme;
  /** Work waiting for sign-off; rendered as a paper stack on the desk. */
  receptionPapers?: number;
  /** Mugs and paper left on desks from today's work. */
  deskTraces?: DeskTrace[];
  /** Department name and level per room. */
  roomBadges?: RoomBadge[];
  /** Department name and level over each seated desk cluster. */
  deskZones?: DeskZone[];
  /** Whether the corner floor plan is shown. */
  minimapVisible?: boolean;
  /** Agent whose panel is open; it pauses and faces the player. */
  engagedAgentId: string | null;
  className?: string;
}

/** Scene commands the page drives from UI outside the canvas. */
export interface OfficeCanvasHandle {
  visitAgent: (agentId: string) => void;
  playEmote: (agentId: string, kind: BubbleKind) => void;
  /** Paper delivery run to the front desk, for a freshly delivered result. */
  deliverToReception: (agentId: string) => void;
  /** Confetti plus a check from every agent. */
  celebrate: () => void;
  /** Player emote above the local avatar. */
  playLocalEmote: (kind: PlayerEmoteId) => void;
  /** Stamped folder, confetti, and a check from every agent. */
  archiveRitual: () => void;
  setMinimapVisible: (visible: boolean) => void;
  /** PNG data URL of the current view. */
  capturePhoto: () => string | null;
}

/**
 * Mounts the Pixi scene. pixi.js is imported on demand: it is by far the
 * heaviest dependency in the app and only this page needs it.
 *
 * The canvas fills its wrapper and the scene owns the camera (follow + wheel
 * zoom), so the world can be much larger than the window.
 */
export const OfficeCanvas = forwardRef<OfficeCanvasHandle, OfficeCanvasProps>(
  function OfficeCanvas(
    {
      agents,
      peers,
      localName,
      roomNames,
      companyName,
      companyLevel,
      spawn,
      onLocalMove,
      onAgentActivate,
      onFocusAgentChange,
      onPlaceFocusChange,
      onRandomEvent,
      season,
      receptionPapers = 0,
      deskTraces = [],
      roomBadges = [],
      deskZones = [],
      minimapVisible = false,
      engagedAgentId,
      className,
    },
    ref,
  ) {
    const wrapperRef = useRef<HTMLDivElement>(null);
    const hostRef = useRef<HTMLDivElement>(null);
    const sceneRef = useRef<OfficeScene | null>(null);
    const callbacks = useRef({
      onLocalMove,
      onAgentActivate,
      onFocusAgentChange,
      onPlaceFocusChange,
      onRandomEvent,
    });
    callbacks.current = {
      onLocalMove,
      onAgentActivate,
      onFocusAgentChange,
      onPlaceFocusChange,
      onRandomEvent,
    };
    const spawnRef = useRef(spawn);
    spawnRef.current = spawn;
    const nameRef = useRef(localName);
    nameRef.current = localName;
    const roomNamesRef = useRef(roomNames);
    roomNamesRef.current = roomNames;
    const companyNameRef = useRef(companyName);
    companyNameRef.current = companyName;
    const companyLevelRef = useRef(companyLevel);
    companyLevelRef.current = companyLevel;
    const seasonRef = useRef(season);
    seasonRef.current = season;

    const [ready, setReady] = useState(false);

    useImperativeHandle(
      ref,
      () => ({
        visitAgent: (agentId) => sceneRef.current?.activateAgent(agentId),
        playEmote: (agentId, kind) => sceneRef.current?.playEmote(agentId, kind),
        deliverToReception: (agentId) =>
          sceneRef.current?.deliverToReception(agentId),
        celebrate: () => sceneRef.current?.celebrate(),
        playLocalEmote: (kind) => sceneRef.current?.playLocalEmote(kind),
        archiveRitual: () => sceneRef.current?.archiveRitual(),
        setMinimapVisible: (visible) => sceneRef.current?.setMinimapVisible(visible),
        capturePhoto: () => sceneRef.current?.capturePhoto() ?? null,
      }),
      [],
    );

    useEffect(() => {
      const wrapper = wrapperRef.current;
      const host = hostRef.current;
      if (!wrapper || !host) return;

      let cancelled = false;
      let scene: OfficeScene | null = null;

      (async () => {
        const pixi = await import("pixi.js");
        if (cancelled) return;
        const rect = wrapper.getBoundingClientRect();
        const created = await OfficeScene.create({
          pixi,
          host,
          width: Math.max(1, Math.round(rect.width)),
          height: Math.max(1, Math.round(rect.height)),
          localName: nameRef.current,
          roomNames: roomNamesRef.current,
          companyName: companyNameRef.current,
          companyLevel: companyLevelRef.current,
          season: seasonRef.current,
          spawn: spawnRef.current,
          onLocalMove: (position) => callbacks.current.onLocalMove(position),
          onAgentActivate: (agentId) =>
            callbacks.current.onAgentActivate(agentId),
          onFocusAgentChange: (agentId) =>
            callbacks.current.onFocusAgentChange(agentId),
          onPlaceFocusChange: (place) =>
            callbacks.current.onPlaceFocusChange?.(place),
          onRandomEvent: (event) => callbacks.current.onRandomEvent?.(event),
        });
        if (cancelled) {
          created.destroy();
          return;
        }
        scene = created;
        sceneRef.current = created;
        setReady(true);
      })().catch(() => {
        // A renderer that cannot start leaves the placeholder in place; the
        // page still works as a roster.
        setReady(false);
      });

      const observer = new ResizeObserver((entries) => {
        const size = entries[0]?.contentRect;
        if (!size || !scene) return;
        scene.resize(size.width, size.height);
      });
      observer.observe(wrapper);

      return () => {
        cancelled = true;
        observer.disconnect();
        scene?.destroy();
        sceneRef.current = null;
        setReady(false);
      };
      // The scene owns its lifetime; data changes are pushed through the
      // effects below instead of remounting the renderer.
    }, []);

    useEffect(() => {
      sceneRef.current?.setPeers(peers);
    }, [peers, ready]);

    useEffect(() => {
      sceneRef.current?.setAgents(agents);
    }, [agents, ready]);

    useEffect(() => {
      sceneRef.current?.setEngagedAgent(engagedAgentId);
    }, [engagedAgentId, ready]);

    useEffect(() => {
      sceneRef.current?.setCompanyLevel(companyLevel);
    }, [companyLevel, ready]);

    useEffect(() => {
      sceneRef.current?.setReceptionPapers(receptionPapers);
    }, [receptionPapers, ready]);

    useEffect(() => {
      sceneRef.current?.setDeskTraces(deskTraces);
    }, [deskTraces, ready]);

    useEffect(() => {
      sceneRef.current?.setRoomBadges(roomBadges);
    }, [roomBadges, ready]);

    useEffect(() => {
      sceneRef.current?.setDeskZones(deskZones);
    }, [deskZones, ready]);

    useEffect(() => {
      sceneRef.current?.setSeason(season);
    }, [season, ready]);

    useEffect(() => {
      sceneRef.current?.setMinimapVisible(minimapVisible);
    }, [minimapVisible, ready]);

    return (
      <div
        ref={wrapperRef}
        className={cn(
          "relative flex min-h-0 w-full flex-1 items-center justify-center overflow-hidden rounded-xl border border-border/60 bg-muted/30 shadow-sm",
          className,
        )}
      >
        <div
          ref={hostRef}
          data-testid="office-canvas"
          className="absolute inset-0 overflow-hidden"
        />
        {!ready && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-body text-muted-foreground">
            <span className="animate-pulse">…</span>
          </div>
        )}
      </div>
    );
  },
);
