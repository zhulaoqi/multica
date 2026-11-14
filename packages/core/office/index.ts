export {
  OFFICE_FRAMES,
  parseOfficeError,
  parseOfficePeer,
  parseOfficePeerLeave,
  parseOfficeState,
  type OfficeStatePayload,
} from "./protocol";
export {
  OFFICE_DIRS,
  OFFICE_HEARTBEAT_MS,
  OFFICE_MOVE_INTERVAL_MS,
  OFFICE_WORLD,
  clampToWorld,
  isOfficeDir,
  type OfficeDir,
  type OfficePeer,
  type OfficePosition,
} from "./types";
export { useOfficeRoom, type OfficeRoom, type UseOfficeRoomOptions } from "./use-office-room";
