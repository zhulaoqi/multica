import { queryOptions, useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import {
  DispatchCandidateSchema,
  type DispatchCandidate,
  type DispatchListItem,
  type DispatchQueueItem,
} from "./schemas";

export const workbenchKeys = {
  all: (wsId: string) => ["workbench", wsId] as const,
  dispatches: (wsId: string) => [...workbenchKeys.all(wsId), "dispatches"] as const,
  departmentQueue: (wsId: string) => [...workbenchKeys.all(wsId), "department-queue"] as const,
  trail: (wsId: string, issueId: string) =>
    [...workbenchKeys.all(wsId), "trail", issueId] as const,
  requestableProjects: (wsId: string) =>
    [...workbenchKeys.all(wsId), "requestable-projects"] as const,
};

/**
 * Projects the caller may hand work. The composer only offers these, so a
 * project without permission is never a dispatch target.
 */
export function requestableProjectsOptions(wsId: string) {
  return queryOptions({
    queryKey: workbenchKeys.requestableProjects(wsId),
    queryFn: () => api.listRequestableProjects(),
    enabled: wsId.length > 0,
    staleTime: 60_000,
  });
}

export const DISPATCH_HISTORY_LIMIT = 30;

export function myDispatchesOptions(wsId: string) {
  return queryOptions({
    queryKey: workbenchKeys.dispatches(wsId),
    queryFn: () => api.listMyDispatches(DISPATCH_HISTORY_LIMIT),
    enabled: wsId.length > 0,
    staleTime: 15_000,
    refetchOnWindowFocus: true,
    // A dispatch delivered into a project workspace does not push its events
    // to the organization socket, so the history polls instead of fanning out
    // across workspaces.
    refetchInterval: 15_000,
  });
}

/** A dispatched root is still moving until a terminal status category lands. */
function dispatchTrailActive(status?: string, category?: string): boolean {
  if (!status) return true;
  if (status === "done" || status === "cancelled") return false;
  return category !== "done" && category !== "closed";
}

/**
 * The requester's trail for one dispatch. The root may live in another
 * workspace, so it is re-read with a light poll until the work settles; once
 * terminal, polling stops and the cached answer stays.
 */
export function dispatchTrailOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: workbenchKeys.trail(wsId, issueId),
    queryFn: () => api.getDispatchTrail(issueId),
    enabled: wsId.length > 0 && issueId.length > 0,
    staleTime: 5_000,
    refetchOnWindowFocus: true,
    refetchInterval: (query) => {
      const issue = query.state.data?.issue;
      return dispatchTrailActive(issue?.status, issue?.status_category) ? 10_000 : false;
    },
  });
}

export function useMyDispatches(wsId: string): {
  dispatches: DispatchListItem[];
  isLoading: boolean;
  error: Error | null;
} {
  const query = useQuery(myDispatchesOptions(wsId));
  return {
    dispatches: query.data ?? [],
    isLoading: query.isLoading,
    error: query.error instanceof Error ? query.error : null,
  };
}

export function departmentDispatchesOptions(wsId: string) {
  return queryOptions({
    queryKey: workbenchKeys.departmentQueue(wsId),
    queryFn: () => api.listDepartmentDispatches(DISPATCH_HISTORY_LIMIT),
    enabled: wsId.length > 0,
    staleTime: 15_000,
    refetchOnWindowFocus: true,
  });
}

export function useDepartmentDispatches(wsId: string): {
  dispatches: DispatchQueueItem[];
  isLoading: boolean;
  error: Error | null;
} {
  const query = useQuery(departmentDispatchesOptions(wsId));
  return {
    dispatches: query.data ?? [],
    isLoading: query.isLoading,
    error: query.error instanceof Error ? query.error : null,
  };
}

/**
 * Candidates from a 422 routing response. Unknown or malformed bodies yield an
 * empty list so the page falls back to its generic error copy.
 */
export function parseDispatchCandidates(error: unknown): DispatchCandidate[] {
  if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return [];
  const candidates = (error.body as { candidates?: unknown }).candidates;
  const parsed = DispatchCandidateSchema.array().safeParse(candidates);
  return parsed.success ? parsed.data : [];
}
