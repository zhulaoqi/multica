"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { statusCategoryOfKey } from "@multica/core/issues";
import { childIssueProgressOptions } from "@multica/core/issues/queries";
import type { DispatchListItem } from "@multica/core/workbench";

/**
 * Where one dispatch currently stands, from the requester's point of view.
 *
 * `progress` means the leader has produced child issues; `in_review` means the
 * department delivered and the requester still has to accept the result. The
 * completion listener treats in_review/done/closed as delivery, so `completed`
 * is the receipt state — either the server already claimed the receipt
 * (`notified_at`) or the issue itself reached a done/closed category.
 */
export type DispatchPhase =
  | "queued"
  | "decomposing"
  | "progress"
  | "in_review"
  | "completed"
  | "failed"
  | "cancelled";

export interface ChildProgress {
  done: number;
  total: number;
}

export function dispatchPhase(
  item: DispatchListItem,
  progress: ChildProgress | undefined,
): DispatchPhase {
  // The workbench schemas parse JSON, so `status_category` is a plain string
  // here; fall back to the built-in key mapping when an older server omits it.
  const category =
    item.issue.status_category ?? statusCategoryOfKey(item.issue.status);
  if (item.issue.status === "cancelled") return "cancelled";
  // The server writes the delivery receipt as soon as the issue reaches review,
  // so review must win over the receipt or the requester never gets to accept.
  if (item.issue.status === "in_review") return "in_review";
  if (item.completed || category === "done" || category === "closed") {
    return "completed";
  }
  if (item.leader_state === "failed") return "failed";
  if (progress && progress.total > 0) return "progress";
  if (item.leader_state === "running" || item.leader_state === "queued") {
    return "decomposing";
  }
  return "queued";
}

/**
 * Progress and phase lookups for the workbench. Child progress is one
 * workspace-wide map keyed by parent issue id, so every history row reads from
 * the same query instead of one request per dispatch.
 */
export function useWorkbenchConversation(wsId: string) {
  const { data: progressMap } = useQuery(childIssueProgressOptions(wsId));
  return useMemo(() => {
    const map = progressMap ?? new Map<string, ChildProgress>();
    return {
      progressFor: (issueId: string): ChildProgress | undefined =>
        map.get(issueId),
      phaseFor: (item: DispatchListItem): DispatchPhase =>
        dispatchPhase(item, map.get(item.issue.id)),
    };
  }, [progressMap]);
}
