import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const vcsKeys = {
  all: (wsId: string) => ["vcs", wsId] as const,
  connections: (wsId: string) => [...vcsKeys.all(wsId), "connections"] as const,
  repositories: (wsId: string, connectionId: string, search: string) =>
    [...vcsKeys.all(wsId), "repositories", connectionId, search] as const,
};

export const vcsConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: vcsKeys.connections(wsId),
    queryFn: () => api.listVCSConnections(wsId),
    enabled: !!wsId,
  });

export const vcsConnectionRepositoriesOptions = (
  wsId: string,
  connectionId: string,
  search = "",
) =>
  infiniteQueryOptions({
    queryKey: vcsKeys.repositories(wsId, connectionId, search),
    queryFn: ({ pageParam }) =>
      api.listVCSConnectionRepositories(wsId, connectionId, {
        page: pageParam,
        per_page: 50,
        search,
      }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.next_page && lastPage.next_page > 0
        ? lastPage.next_page
        : undefined,
    enabled: !!wsId && !!connectionId,
  });
