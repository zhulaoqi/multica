import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import type { TimelineEntry } from "@multica/core/types";

const PLATFORM_COMMENT_TYPES = new Set(["system", "status_change"]);

/**
 * The department's answer on a dispatch: the newest comment an agent wrote on
 * the root issue. The lead posts its delivery summary there when it moves the
 * issue to review, so the latest agent comment is the result the requester
 * came back for. Platform notices (run failures, status changes) and deleted
 * tombstones are not results.
 */
export function dispatchResult(entries: readonly TimelineEntry[]): TimelineEntry | null {
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i]!;
    if (entry.type !== "comment" || entry.actor_type !== "agent") continue;
    if (PLATFORM_COMMENT_TYPES.has(entry.comment_type ?? "")) continue;
    if (isDeletedComment(entry) || !entry.content?.trim()) continue;
    return entry;
  }
  return null;
}
