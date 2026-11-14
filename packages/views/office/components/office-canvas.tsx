"use client";

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { cn } from "@multica/ui/lib/utils";
import { OFFICE_WORLD, type OfficePosition } from "@multica/core/office";
import type { BubbleKind } from "../scene/characters";
import {
  OfficeScene,
  type OfficeAgentView,
  type OfficePeerView,
} from "../scene/office-scene";

export interface OfficeCanvasProps {
  agents: OfficeAgentView[];
  peers: OfficePeerView[];
  localName: string;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  /** The player reached an agent (walked over or clicked it). */
  onAgentActivate: (agentId: string) => void;
  /** Nearest agent changed, for the "press T" hint. */
  onFocusAgentChange: (agentId: string | null) => void;
  /** Agent whose panel is open; it stops wandering and faces the player. */
  engagedAgentId: string | null;
  className?: string;
}

/** Scene commands the page drives from UI outside the canvas. */
export interface OfficeCanvasHandle {
  visitAgent: (agentId: string) => void;
  playEmote: (agentId: string, kind: BubbleKind) => void;
}

const MIN_ZOOM = 1;
const MAX_ZOOM = 3;

/** Rounds to half steps so the pixel grid stays crisp at any container size. */
function fitZoom(width: number, height: number): number {
  if (width <= 0 || height <= 0) return MIN_ZOOM;
  const fit = Math.min(
    (width - 24) / OFFICE_WORLD.width,
    (height - 24) / OFFICE_WORLD.height,
  );
  const halfStepped = Math.floor(fit * 2) / 2;
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, halfStepped));
}

/**
 * Mounts the Pixi scene. pixi.js is imported on demand: it is by far the
 * heaviest dependency in the app and only this page needs it.
 */
export const OfficeCanvas = forwardRef<OfficeCanvasHandle, OfficeCanvasProps>(
  function OfficeCanvas(
    {
      agents,
      peers,
      localName,
      spawn,
      onLocalMove,
      onAgentActivate,
      onFocusAgentChange,
      engagedAgentId,
      className,
    },
    ref,
  ) {
    const wrapperRef = useRef<HTMLDivElement>(null);
    const hostRef = useRef<HTMLDivElement>(null);
    const sceneRef = useRef<OfficeScene | null>(null);
    const callbacks = useRef({ onLocalMove, onAgentActivate, onFocusAgentChange });
    callbacks.current = { onLocalMove, onAgentActivate, onFocusAgentChange };
    const spawnRef = useRef(spawn);
    spawnRef.current = spawn;
    const nameRef = useRef(localName);
    nameRef.current = localName;

    const [ready, setReady] = useState(false);
    const [zoom, setZoom] = useState(MIN_ZOOM);

    useImperativeHandle(
      ref,
      () => ({
        visitAgent: (agentId) => sceneRef.current?.activateAgent(agentId),
        playEmote: (agentId, kind) => sceneRef.current?.playEmote(agentId, kind),
      }),
      [],
    );

    useEffect(() => {
      const wrapper = wrapperRef.current;
      if (!wrapper) return;
      const observer = new ResizeObserver((entries) => {
        const rect = entries[0]?.contentRect;
        if (rect) setZoom(fitZoom(rect.width, rect.height));
      });
      observer.observe(wrapper);
      return () => observer.disconnect();
    }, []);

    useEffect(() => {
      const host = hostRef.current;
      if (!host) return;

      let cancelled = false;
      let scene: OfficeScene | null = null;

      (async () => {
        const pixi = await import("pixi.js");
        if (cancelled) return;
        const created = await OfficeScene.create({
          pixi,
          host,
          zoom,
          localName: nameRef.current,
          spawn: spawnRef.current,
          onLocalMove: (position) => callbacks.current.onLocalMove(position),
          onAgentActivate: (agentId) =>
            callbacks.current.onAgentActivate(agentId),
          onFocusAgentChange: (agentId) =>
            callbacks.current.onFocusAgentChange(agentId),
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

      return () => {
        cancelled = true;
        scene?.destroy();
        sceneRef.current = null;
        setReady(false);
      };
      // The scene owns its lifetime; data and zoom changes are pushed through
      // the effects below instead of remounting the renderer.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
      sceneRef.current?.setZoom(zoom);
    }, [zoom, ready]);

    useEffect(() => {
      sceneRef.current?.setPeers(peers);
    }, [peers, ready]);

    useEffect(() => {
      sceneRef.current?.setAgents(agents);
    }, [agents, ready]);

    useEffect(() => {
      sceneRef.current?.setEngagedAgent(engagedAgentId);
    }, [engagedAgentId, ready]);

    return (
      <div
        ref={wrapperRef}
        className={cn(
          "flex min-h-0 w-full flex-1 items-center justify-center overflow-hidden",
          className,
        )}
      >
        <div
          ref={hostRef}
          data-testid="office-canvas"
          className="relative shrink-0 overflow-hidden rounded-xl border border-border/60 bg-muted/30 shadow-sm"
          style={{
            width: Math.round(OFFICE_WORLD.width * zoom),
            height: Math.round(OFFICE_WORLD.height * zoom),
          }}
        >
          {!ready && (
            <div className="absolute inset-0 flex items-center justify-center text-body text-muted-foreground">
              <span className="animate-pulse">…</span>
            </div>
          )}
        </div>
      </div>
    );
  },
);
