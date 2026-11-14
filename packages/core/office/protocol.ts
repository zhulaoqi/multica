import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import { isOfficeDir, type OfficePeer } from "./types";

/**
 * Frame names of the office protocol. They travel on the shared app socket
 * but are not server → client events, so they are declared here instead of
 * being added to the global `WSEventType` union.
 */
export const OFFICE_FRAMES = {
  join: "office_join",
  move: "office_move",
  leave: "office_leave",
  state: "office_state",
  peerJoin: "office_peer_join",
  peerMove: "office_peer_move",
  peerLeave: "office_peer_leave",
  error: "office_error",
} as const;

const officeMemberSchema = z.object({
  user_id: z.string().min(1),
  x: z.number(),
  y: z.number(),
  // The server sends "" when the facing direction is unknown.
  dir: z.string().nullish(),
});

// The envelope is validated here; members are validated one by one so a
// single malformed avatar degrades to a missing avatar, not an empty room.
const officeStateSchema = z.object({
  members: z.array(z.unknown()),
});

const officePeerLeaveSchema = z.object({
  user_id: z.string().min(1),
});

const officeErrorSchema = z.object({
  error: z.string().min(1),
});

export interface OfficeStatePayload {
  members: OfficePeer[];
}

type RawMember = z.infer<typeof officeMemberSchema>;

function toPeer(member: RawMember): OfficePeer | null {
  if (!Number.isFinite(member.x) || !Number.isFinite(member.y)) return null;
  return {
    userId: member.user_id,
    x: member.x,
    y: member.y,
    dir: isOfficeDir(member.dir) ? member.dir : null,
  };
}

/** Validates an `office_peer_join` / `office_peer_move` payload. */
export function parseOfficePeer(data: unknown): OfficePeer | null {
  const parsed = officeMemberSchema.safeParse(data);
  if (!parsed.success) return null;
  return toPeer(parsed.data);
}

/** Validates an `office_state` snapshot; malformed members are dropped. */
export function parseOfficeState(data: unknown): OfficeStatePayload {
  const parsed = parseWithFallback(
    data,
    officeStateSchema,
    { members: [] },
    { endpoint: "ws:office_state" },
  );
  const members: OfficePeer[] = [];
  for (const member of parsed.members) {
    const peer = parseOfficePeer(member);
    if (peer) members.push(peer);
  }
  return { members };
}

export function parseOfficePeerLeave(data: unknown): string | null {
  const parsed = officePeerLeaveSchema.safeParse(data);
  return parsed.success ? parsed.data.user_id : null;
}

export function parseOfficeError(data: unknown): string | null {
  const parsed = officeErrorSchema.safeParse(data);
  return parsed.success ? parsed.data.error : null;
}
