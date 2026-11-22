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
