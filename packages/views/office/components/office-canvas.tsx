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
  type OfficeAgentView,
  type OfficePeerView,
} from "../scene/office-scene";

export interface OfficeCanvasProps {
  agents: OfficeAgentView[];
  peers: OfficePeerView[];
  localName: string;
  roomNames: Record<RoomId, string>;
  companyName: string;
  spawn: OfficePosition;
  onLocalMove: (position: OfficePosition) => void;
  /** The player reached an agent (walked over or clicked it). */
  onAgentActivate: (agentId: string) => void;
  /** Nearest agent changed, for the interact hint. */
  onFocusAgentChange: (agentId: string | null) => void;
  /** Agent whose panel is open; it pauses and faces the player. */
  engagedAgentId: string | null;
  className?: string;
}

/** Scene commands the page drives from UI outside the canvas. */
export interface OfficeCanvasHandle {
  visitAgent: (agentId: string) => void;
  playEmote: (agentId: string, kind: BubbleKind) => void;
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
    const roomNamesRef = useRef(roomNames);
    roomNamesRef.current = roomNames;
    const companyNameRef = useRef(companyName);
    companyNameRef.current = companyName;

    const [ready, setReady] = useState(false);

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
