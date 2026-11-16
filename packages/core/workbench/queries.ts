import { queryOptions, useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { DispatchCandidateSchema, type DispatchCandidate, type DispatchListItem } from "./schemas";

export const workbenchKeys = {
  all: (wsId: string) => ["workbench", wsId] as const,
  dispatches: (wsId: string) => [...workbenchKeys.all(wsId), "dispatches"] as const,
};

export const DISPATCH_HISTORY_LIMIT = 30;

export function myDispatchesOptions(wsId: string) {
  return queryOptions({
    queryKey: workbenchKeys.dispatches(wsId),
    queryFn: () => api.listMyDispatches(DISPATCH_HISTORY_LIMIT),
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
