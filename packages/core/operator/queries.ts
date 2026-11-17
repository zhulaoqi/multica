import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for the platform console. Account-level (no wsId):
 * the overview spans every workspace at once. */
export const operatorKeys = {
  all: ["operator"] as const,
  overview: () => [...operatorKeys.all, "overview"] as const,
  directories: () => [...operatorKeys.all, "directories"] as const,
};

export const operatorOverviewOptions = () =>
  queryOptions({
    queryKey: operatorKeys.overview(),
    queryFn: () => api.getOperatorOverview(),
    staleTime: 60_000,
  });

export const operatorDirectoryOptions = () =>
  queryOptions({
    queryKey: operatorKeys.directories(),
    queryFn: () => api.getOperatorDirectories(),
    staleTime: 30_000,
  });
