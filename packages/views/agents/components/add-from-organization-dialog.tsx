"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { errorCode } from "@multica/core/api";
import { useAddAgentProjections } from "@multica/core/workspace/mutations";
import { memberListOptions, projectionCandidatesOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";

/**
 * Adds organization agents owned by project members to a project. The project
 * keeps read-only projections of them; their configuration stays in the
 * organization.
 */
export function AddFromOrganizationDialog({
  wsId,
  open,
  onOpenChange,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("agents");
  const candidatesQuery = useQuery(projectionCandidatesOptions(wsId, open));
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: open && !!wsId });
  const addProjections = useAddAgentProjections(wsId);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    if (open) setSelected(new Set());
  }, [open]);

  const candidates = candidatesQuery.data ?? [];
  const available = candidates.filter((candidate) => !candidate.projected);
  const ownerName = (ownerId: string) =>
    members.find((member) => member.user_id === ownerId)?.name ?? "";
  const submitting = addProjections.isPending;

  const toggle = (id: string, checked: boolean) => {
    setSelected((current) => {
      const next = new Set(current);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const handleAdd = () => {
    addProjections.mutate([...selected], {
      onSuccess: (agents) => {
        toast.success(t(($) => $.add_from_organization.added_toast, { count: agents.length }));
        onOpenChange(false);
      },
      onError: (error) => {
        toast.error(
          errorCode(error) === "organization_missing"
            ? t(($) => $.add_from_organization.organization_missing)
            : t(($) => $.add_from_organization.add_failed),
        );
      },
    });
  };

  let body: React.ReactNode;
  if (candidatesQuery.isLoading) {
    body = (
      <div className="space-y-2">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
      </div>
    );
  } else if (candidatesQuery.isError) {
    body = (
      <p role="alert" className="text-caption text-destructive">
        {errorCode(candidatesQuery.error) === "organization_missing"
          ? t(($) => $.add_from_organization.organization_missing)
          : t(($) => $.add_from_organization.load_failed)}
      </p>
    );
  } else if (available.length === 0) {
    body = (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.add_from_organization.empty)}
      </p>
    );
  } else {
    body = (
      <ul className="max-h-72 space-y-0.5 overflow-y-auto">
        {available.map((candidate) => {
          const owner = ownerName(candidate.owner_id);
          return (
            <li key={candidate.id}>
              <label className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted/60">
                <Checkbox
                  checked={selected.has(candidate.id)}
                  onCheckedChange={(next) => toggle(candidate.id, next === true)}
                  disabled={submitting}
                />
                <ActorAvatar
                  name={candidate.name}
                  initials={candidate.name.charAt(0).toUpperCase()}
                  avatarUrl={resolvePublicFileUrl(candidate.avatar_url)}
                  isAgent
                  size="sm"
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-body">{candidate.name}</span>
                  {owner && (
                    <span className="block truncate text-caption text-muted-foreground">
                      {candidate.is_primary
                        ? t(($) => $.add_from_organization.primary_twin_of, { name: owner })
                        : owner}
                    </span>
                  )}
                </span>
              </label>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !submitting && onOpenChange(next)}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.add_from_organization.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.add_from_organization.description)}</DialogDescription>
        </DialogHeader>
        {body}
        <DialogFooter>
          <DialogClose render={<Button variant="outline" disabled={submitting} />}>
            {t(($) => $.add_from_organization.cancel)}
          </DialogClose>
          <Button onClick={handleAdd} disabled={selected.size === 0 || submitting}>
            {t(($) => $.add_from_organization.confirm, { count: selected.size })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
