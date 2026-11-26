"use client";

import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { LoaderCircle, Search } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import type { VCSConnection, VCSRepository, WorkspaceRepo } from "@multica/core/types";
import { vcsConnectionRepositoriesOptions } from "@multica/core/vcs/queries";
import { useT } from "../../i18n";
import { repositoryIdentity } from "./repository-identity";

type RepositoryConnection = Pick<
  VCSConnection,
  "id" | "provider" | "instance_url" | "account_login"
>;

interface VCSRepositoryPickerProps {
  open: boolean;
  workspaceId: string;
  connections: RepositoryConnection[];
  initialConnectionId?: string | null;
  existingUrls: string[];
  onOpenChange: (open: boolean) => void;
  onImport: (repositories: WorkspaceRepo[]) => Promise<boolean> | boolean;
}

export function VCSRepositoryPicker({
  open,
  workspaceId,
  connections,
  initialConnectionId,
  existingUrls,
  onOpenChange,
  onImport,
}: VCSRepositoryPickerProps) {
  const { t } = useT("settings");
  const [selectedConnectionId, setSelectedConnectionId] = useState("");
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const [selected, setSelected] = useState<Map<number, VCSRepository>>(new Map());
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    const requested = connections.some((connection) => connection.id === initialConnectionId)
      ? initialConnectionId!
      : connections[0]?.id ?? "";
    setSelectedConnectionId(requested);
    setSearch("");
    setSelected(new Map());
  }, [connections, initialConnectionId, open]);

  const query = useInfiniteQuery({
    ...vcsConnectionRepositoriesOptions(
      workspaceId,
      selectedConnectionId,
      deferredSearch,
    ),
    enabled: open && !!workspaceId && !!selectedConnectionId,
  });
  const repositories = useMemo(
    () => query.data?.pages.flatMap((page) => page.repositories) ?? [],
    [query.data?.pages],
  );
  const existingIdentities = useMemo(
    () =>
      new Set(
        existingUrls
          .map(repositoryIdentity)
          .filter((identity): identity is string => !!identity),
      ),
    [existingUrls],
  );

  function close() {
    if (saving) return;
    onOpenChange(false);
  }

  function toggle(repository: VCSRepository, checked: boolean) {
    setSelected((current) => {
      const next = new Map(current);
      if (checked) next.set(repository.id, repository);
      else next.delete(repository.id);
      return next;
    });
  }

  async function importRepositories() {
    if (selected.size === 0 || saving) return;
    setSaving(true);
    try {
      const latestById = new Map(
        repositories.map((repository) => [repository.id, repository]),
      );
      const seenIdentities = new Set(existingIdentities);
      const additions: WorkspaceRepo[] = [];
      for (const selectedRepository of selected.values()) {
        const repository = latestById.get(selectedRepository.id) ?? selectedRepository;
        const identity = repositoryIdentity(repository.clone_url);
        if (
          repository.archived ||
          (identity && seenIdentities.has(identity))
        ) {
          continue;
        }
        if (identity) seenIdentities.add(identity);
        additions.push({
          url: repository.clone_url,
          ...(repository.description.trim()
            ? { description: repository.description.trim() }
            : {}),
        });
      }
      if (additions.length === 0) {
        onOpenChange(false);
        return;
      }
      if (await onImport(additions)) onOpenChange(false);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
      }}
    >
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-2xl">
        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle>{t(($) => $.repositories.gitlab_picker_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.repositories.gitlab_picker_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3 px-6 py-4">
          {connections.length > 1 ? (
            <Select
              items={connections.map((connection) => ({
                value: connection.id,
                label: connection.instance_url,
              }))}
              value={selectedConnectionId}
              onValueChange={(value) => {
                setSelectedConnectionId(value ?? "");
                setSelected(new Map());
              }}
            >
              <SelectTrigger aria-label={t(($) => $.repositories.gitlab_instance)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {connections.map((connection) => (
                  <SelectItem key={connection.id} value={connection.id}>
                    {connection.instance_url}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : connections[0] ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.repositories.gitlab_instance)}:{" "}
              <span className="font-medium text-foreground">
                {connections[0].instance_url}
              </span>
            </p>
          ) : null}

          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t(($) => $.repositories.gitlab_search_placeholder)}
              aria-label={t(($) => $.repositories.gitlab_search_placeholder)}
              className="pl-8"
            />
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto border-y">
          {query.isPending ? (
            <div className="flex items-center justify-center gap-2 px-6 py-12 text-body text-muted-foreground">
              <LoaderCircle className="size-4 animate-spin" />
              {t(($) => $.repositories.gitlab_loading)}
            </div>
          ) : query.isError ? (
            <div className="px-6 py-12 text-center text-body text-muted-foreground">
              {t(($) => $.repositories.gitlab_load_failed)}
            </div>
          ) : repositories.length === 0 ? (
            <div className="px-6 py-12 text-center text-body text-muted-foreground">
              {deferredSearch
                ? t(($) => $.repositories.gitlab_no_search_results)
                : t(($) => $.repositories.gitlab_empty)}
            </div>
          ) : (
            <div className="divide-y">
              {repositories.map((repository) => {
                const identity = repositoryIdentity(repository.clone_url);
                const alreadyAdded =
                  !!identity && existingIdentities.has(identity);
                const disabled = alreadyAdded || repository.archived;
                return (
                  <label
                    key={repository.id}
                    htmlFor={`gitlab-repository-${repository.id}`}
                    className="flex items-start gap-3 px-6 py-3.5"
                  >
                    <Checkbox
                      id={`gitlab-repository-${repository.id}`}
                      checked={alreadyAdded || selected.has(repository.id)}
                      disabled={disabled}
                      onCheckedChange={(checked) =>
                        toggle(repository, checked === true)
                      }
                      className="mt-0.5"
                    />
                    <span className="min-w-0 flex-1 space-y-1">
                      <span className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-body font-medium">
                          {repository.full_name}
                        </span>
                        {repository.private ? (
                          <Badge variant="secondary">
                            {t(($) => $.repositories.gitlab_private)}
                          </Badge>
                        ) : null}
                        {repository.archived ? (
                          <Badge variant="outline">
                            {t(($) => $.repositories.gitlab_archived)}
                          </Badge>
                        ) : null}
                        {alreadyAdded ? (
                          <Badge variant="outline">
                            {t(($) => $.repositories.gitlab_added)}
                          </Badge>
                        ) : null}
                      </span>
                      {repository.description ? (
                        <span className="block truncate text-caption text-muted-foreground">
                          {repository.description}
                        </span>
                      ) : null}
                    </span>
                  </label>
                );
              })}
            </div>
          )}

          {query.hasNextPage ? (
            <div className="flex justify-center border-t p-3">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => void query.fetchNextPage()}
                disabled={query.isFetchingNextPage}
              >
                {query.isFetchingNextPage
                  ? t(($) => $.repositories.gitlab_loading)
                  : t(($) => $.repositories.gitlab_load_more)}
              </Button>
            </div>
          ) : null}
        </div>

        <DialogFooter className="m-0 border-t bg-muted/30 px-6 py-4">
          <p className="mr-auto text-caption text-muted-foreground">
            {t(($) => $.repositories.gitlab_selected_count, {
              count: selected.size,
            })}
          </p>
          <Button variant="ghost" onClick={close} disabled={saving}>
            {t(($) => $.repositories.gitlab_cancel)}
          </Button>
          <Button
            onClick={() => void importRepositories()}
            disabled={selected.size === 0 || saving}
            aria-busy={saving || undefined}
          >
            {t(($) => $.repositories.gitlab_import)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
