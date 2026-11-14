"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useOptionalWS } from "../realtime";
import {
  OFFICE_FRAMES,
  parseOfficeError,
  parseOfficePeer,
  parseOfficePeerLeave,
  parseOfficeState,
} from "./protocol";
import {
  OFFICE_HEARTBEAT_MS,
  OFFICE_MOVE_INTERVAL_MS,
  type OfficeDir,
  type OfficePeer,
  type OfficePosition,
} from "./types";

export interface UseOfficeRoomOptions {
  /** Signed-in member. The room is only joined once this is known. */
  currentUserId: string | null;
  /** Where the local avatar appears on first join. */
  spawn: OfficePosition;
  /** Set false to stay out of the room (e.g. while the scene loads). */
  enabled?: boolean;
}

export interface OfficeRoom {
  /** Other members currently in the room. Never includes the local user. */
  peers: OfficePeer[];
  /** True while the socket is authenticated and the join frame has been sent. */
  joined: boolean;
  /** Last server-side rejection, e.g. `invalid_position`. Cleared on rejoin. */
  error: string | null;
  /** Throttled to 8 Hz; the latest position always wins. */
  sendMove: (x: number, y: number, dir: OfficeDir | null) => void;
}

/**
 * Joins the workspace office room and mirrors remote members into React state.
 *
 * The local avatar is not part of `peers`: its position is driven by the
 * scene (movement, collision) and only reported to the server, so a slow
 * echo can never drag the local sprite backwards.
 */
export function useOfficeRoom({
  currentUserId,
  spawn,
  enabled = true,
}: UseOfficeRoomOptions): OfficeRoom {
  const ws = useOptionalWS();
  const connected = ws?.connected ?? false;
  const sendFrame = ws?.sendFrame;
  const onFrame = ws?.onFrame;

  const [peers, setPeers] = useState<OfficePeer[]>([]);
  const [joined, setJoined] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const positionRef = useRef<OfficePosition>(spawn);
  const spawnRef = useRef(spawn);
  spawnRef.current = spawn;

  const pendingRef = useRef<OfficePosition | null>(null);
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastSentAtRef = useRef(0);

  const flushMove = useCallback(() => {
    flushTimerRef.current = null;
    const pending = pendingRef.current;
    if (!pending || !sendFrame) return;
    pendingRef.current = null;
    lastSentAtRef.current = Date.now();
    positionRef.current = pending;
    sendFrame(OFFICE_FRAMES.move, {
      x: pending.x,
      y: pending.y,
      dir: pending.dir ?? "",
    });
  }, [sendFrame]);

  const sendMove = useCallback(
    (x: number, y: number, dir: OfficeDir | null) => {
      pendingRef.current = { x, y, dir };
      const elapsed = Date.now() - lastSentAtRef.current;
      if (elapsed >= OFFICE_MOVE_INTERVAL_MS) {
        flushMove();
        return;
      }
      if (!flushTimerRef.current) {
        flushTimerRef.current = setTimeout(
          flushMove,
          OFFICE_MOVE_INTERVAL_MS - elapsed,
        );
      }
    },
    [flushMove],
  );

  useEffect(() => {
    if (!enabled || !connected || !currentUserId || !sendFrame || !onFrame) {
      return;
    }

    const members = new Map<string, OfficePeer>();
    const publish = () => setPeers([...members.values()]);
    const upsert = (peer: OfficePeer | null) => {
      if (!peer || peer.userId === currentUserId) return;
      members.set(peer.userId, peer);
      publish();
    };

    const offState = onFrame(OFFICE_FRAMES.state, (payload) => {
      const snapshot = parseOfficeState(payload);
      members.clear();
      for (const peer of snapshot.members) {
        if (peer.userId !== currentUserId) members.set(peer.userId, peer);
      }
      publish();
    });
    const offJoin = onFrame(OFFICE_FRAMES.peerJoin, (payload) => {
      upsert(parseOfficePeer(payload));
    });
    const offMove = onFrame(OFFICE_FRAMES.peerMove, (payload) => {
      upsert(parseOfficePeer(payload));
    });
    const offLeave = onFrame(OFFICE_FRAMES.peerLeave, (payload) => {
      const userId = parseOfficePeerLeave(payload);
      if (!userId) return;
      if (members.delete(userId)) publish();
    });
    const offError = onFrame(OFFICE_FRAMES.error, (payload) => {
      setError(parseOfficeError(payload));
    });

    // Re-announcing counts as a position update on the server and refills the
    // peer snapshot, which heals a missed frame without a separate resync.
    const announce = () => {
      const position = positionRef.current ?? spawnRef.current;
      sendFrame(OFFICE_FRAMES.join, {
        x: position.x,
        y: position.y,
        dir: position.dir ?? "",
      });
    };

    setError(null);
    announce();
    setJoined(true);
    const heartbeat = setInterval(announce, OFFICE_HEARTBEAT_MS);

    return () => {
      clearInterval(heartbeat);
      setJoined(false);
      sendFrame(OFFICE_FRAMES.leave, null);
      offState();
      offJoin();
      offMove();
      offLeave();
      offError();
      members.clear();
      setPeers([]);
    };
  }, [enabled, connected, currentUserId, sendFrame, onFrame]);

  useEffect(() => {
    return () => {
      if (flushTimerRef.current) clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    };
  }, []);

  return { peers, joined, error, sendMove };
}
