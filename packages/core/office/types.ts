/**
 * Shared vocabulary for the virtual office room.
 *
 * The server owns membership and rejects anything outside the world bounds;
 * these constants exist so the client can lay out the room and clamp its own
 * avatar before sending. They must stay in sync with
 * `server/internal/realtime/office.go`.
 */

export const OFFICE_WORLD = {
  /** Logical world size in pixels. */
  width: 480,
  height: 288,
  /** Tile size the room is drawn on (30 x 18 tiles). */
  tile: 16,
} as const;

/** Uplink cadence for the local avatar: 8 frames per second. */
export const OFFICE_MOVE_INTERVAL_MS = 125;

/**
 * Re-announce interval. Re-sending the join frame refills the peer snapshot,
 * so a dropped frame or a silently dead socket heals within one interval.
 */
export const OFFICE_HEARTBEAT_MS = 5_000;

export const OFFICE_DIRS = ["up", "down", "left", "right"] as const;

export type OfficeDir = (typeof OFFICE_DIRS)[number];

export interface OfficePosition {
  x: number;
  y: number;
  dir: OfficeDir | null;
}

export interface OfficePeer extends OfficePosition {
  userId: string;
}

export function isOfficeDir(value: unknown): value is OfficeDir {
  return typeof value === "string" && (OFFICE_DIRS as readonly string[]).includes(value);
}

/** Clamps a coordinate into the world, ignoring non-finite input. */
export function clampToWorld(x: number, y: number): { x: number; y: number } {
  const safeX = Number.isFinite(x) ? x : 0;
  const safeY = Number.isFinite(y) ? y : 0;
  return {
    x: Math.min(Math.max(safeX, 0), OFFICE_WORLD.width),
    y: Math.min(Math.max(safeY, 0), OFFICE_WORLD.height),
  };
}
