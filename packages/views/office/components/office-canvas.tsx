"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { OFFICE_WORLD, type OfficePosition } from "@multica/core/office";
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
  onAgentClick: (agentId: string) => void;
  className?: string;
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
export function OfficeCanvas({
  agents,
  peers,
  localName,
  spawn,
  onLocalMove,
  onAgentClick,
  className,
}: OfficeCanvasProps) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const hostRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<OfficeScene | null>(null);
  const callbacks = useRef({ onLocalMove, onAgentClick });
  callbacks.current = { onLocalMove, onAgentClick };
  const spawnRef = useRef(spawn);
  spawnRef.current = spawn;
  const nameRef = useRef(localName);
  nameRef.current = localName;

  const [ready, setReady] = useState(false);
  const [zoom, setZoom] = useState(MIN_ZOOM);

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
        onAgentClick: (agentId) => callbacks.current.onAgentClick(agentId),
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
    // The scene owns its lifetime; data and zoom changes are pushed through the
    // effects below instead of remounting the renderer.
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
}
