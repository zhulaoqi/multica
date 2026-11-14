/**
 * @vitest-environment jsdom
 */
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OFFICE_FRAMES } from "./protocol";
import type { OfficePosition } from "./types";
import { useOfficeRoom } from "./use-office-room";

type FrameHandler = (payload: unknown) => void;

const ws = vi.hoisted(() => {
  const handlers = new Map<string, FrameHandler>();
  return {
    connected: true,
    sendFrame: vi.fn(),
    handlers,
    onFrame: vi.fn((type: string, handler: FrameHandler) => {
      handlers.set(type, handler);
      return () => handlers.delete(type);
    }),
  };
});

vi.mock("../realtime", () => ({
  useOptionalWS: () => ({
    connected: ws.connected,
    sendFrame: ws.sendFrame,
    onFrame: ws.onFrame,
  }),
}));

const SPAWN: OfficePosition = { x: 40, y: 60, dir: "down" };

function emit(type: string, payload: unknown) {
  const handler = ws.handlers.get(type);
  if (!handler) throw new Error(`no handler for ${type}`);
  act(() => handler(payload));
}

function joinCalls() {
  return ws.sendFrame.mock.calls.filter(([type]) => type === OFFICE_FRAMES.join);
}

function moveCalls() {
  return ws.sendFrame.mock.calls.filter(([type]) => type === OFFICE_FRAMES.move);
}

describe("useOfficeRoom", () => {
  beforeEach(() => {
    ws.connected = true;
    ws.handlers.clear();
    ws.sendFrame.mockClear();
    ws.onFrame.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("joins at the spawn position and leaves on unmount", () => {
    const { unmount } = renderHook(() =>
      useOfficeRoom({ currentUserId: "me", spawn: SPAWN }),
    );

    expect(ws.sendFrame).toHaveBeenCalledWith(OFFICE_FRAMES.join, {
      x: SPAWN.x,
      y: SPAWN.y,
      dir: "down",
    });

    unmount();
    expect(ws.sendFrame).toHaveBeenCalledWith(OFFICE_FRAMES.leave, null);
    expect(ws.handlers.size).toBe(0);
  });

  it("does not join while the socket is disconnected", () => {
    ws.connected = false;
    const { result } = renderHook(() =>
      useOfficeRoom({ currentUserId: "me", spawn: SPAWN }),
    );

    expect(joinCalls()).toHaveLength(0);
    expect(result.current.joined).toBe(false);
  });

  it("mirrors peers and never includes the local user", () => {
    const { result } = renderHook(() =>
      useOfficeRoom({ currentUserId: "me", spawn: SPAWN }),
    );

    emit(OFFICE_FRAMES.state, {
      members: [
        { user_id: "me", x: 1, y: 2, dir: "up" },
        { user_id: "u1", x: 10, y: 20, dir: "left" },
      ],
    });
    expect(result.current.peers).toEqual([
      { userId: "u1", x: 10, y: 20, dir: "left" },
    ]);

    emit(OFFICE_FRAMES.peerJoin, { user_id: "u2", x: 30, y: 40, dir: "right" });
    expect(result.current.peers.map((peer) => peer.userId)).toEqual(["u1", "u2"]);

    emit(OFFICE_FRAMES.peerMove, { user_id: "u1", x: 11, y: 21, dir: "down" });
    expect(result.current.peers[0]).toEqual({
      userId: "u1",
      x: 11,
      y: 21,
      dir: "down",
    });

    emit(OFFICE_FRAMES.peerLeave, { user_id: "u1" });
    expect(result.current.peers.map((peer) => peer.userId)).toEqual(["u2"]);
  });

  it("drops malformed peer frames instead of rendering them", () => {
    const { result } = renderHook(() =>
      useOfficeRoom({ currentUserId: "me", spawn: SPAWN }),
    );

    emit(OFFICE_FRAMES.peerJoin, { user_id: "", x: 1, y: 2 });
    emit(OFFICE_FRAMES.peerJoin, { user_id: "u1", x: "nope", y: 2 });
    emit(OFFICE_FRAMES.peerMove, null);
    emit(OFFICE_FRAMES.state, "garbage");
    emit(OFFICE_FRAMES.error, { error: "invalid_position" });

    expect(result.current.peers).toEqual([]);
    expect(result.current.error).toBe("invalid_position");
  });

  it("throttles moves to 8 Hz and always sends the latest position", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(0));
    const { result } = renderHook(() =>
      useOfficeRoom({ currentUserId: "me", spawn: SPAWN }),
    );

    act(() => {
      result.current.sendMove(1, 1, "up");
      result.current.sendMove(2, 2, "up");
      result.current.sendMove(3, 3, "up");
    });
    expect(moveCalls()).toHaveLength(0);

    act(() => {
      vi.advanceTimersByTime(125);
    });
    expect(moveCalls()).toHaveLength(1);
    expect(moveCalls()[0]![1]).toEqual({ x: 3, y: 3, dir: "up" });

    act(() => {
      result.current.sendMove(4, 4, "down");
    });
    act(() => {
      vi.advanceTimersByTime(125);
    });
    expect(moveCalls()).toHaveLength(2);
    expect(moveCalls()[1]![1]).toEqual({ x: 4, y: 4, dir: "down" });
  });

  it("re-announces the last position on the heartbeat", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(0));
    renderHook(() => useOfficeRoom({ currentUserId: "me", spawn: SPAWN }));

    act(() => {
      vi.advanceTimersByTime(5_000);
    });

    expect(joinCalls()).toHaveLength(2);
    expect(joinCalls()[1]![1]).toEqual({ x: SPAWN.x, y: SPAWN.y, dir: "down" });
  });
});
