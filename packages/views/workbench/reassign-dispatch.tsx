"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import {
  useReassignDispatch,
  type DispatchListItem,
} from "@multica/core/workbench";
import { orgTreeOptions } from "@multica/core/workspace/queries";
import { useT } from "../i18n";
import { unitPaths, unitSubtreeIDs } from "../org/org-tree";
import { DispatchErrorText } from "./dispatch-conversation";

/**
 * Moves a dispatch the requester sent to another department. The list only
 * shows departments that could actually pick it up, because a target without
 * agents or with an offline leader would be rejected by the server.
 */
export function ReassignDispatchDialog({
  wsId,
  item,
  open,
  onOpenChange,
}: {
  wsId: string;
  item: DispatchListItem;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workbench");
  const reassign = useReassignDispatch(wsId);
  const [target, setTarget] = useState("");

  const { data: tree } = useQuery({ ...orgTreeOptions(wsId), enabled: open });

  const options = useMemo(() => {
    const units = tree?.units ?? [];
    const agents = tree?.agents ?? [];
    const paths = unitPaths(units);
    return units
      .filter((unit) => unit.id !== item.unit_id)
      .map((unit) => {
        const subtree = unitSubtreeIDs(units, unit.id);
        const count = agents.filter(
          (agent) => agent.unit_id && subtree.has(agent.unit_id),
        ).length;
        return {
          id: unit.id,
          path: paths.get(unit.id) ?? unit.name,
          count,
        };
      })
      .sort((a, b) => a.path.localeCompare(b.path));
  }, [item.unit_id, tree]);

  const close = () => {
    if (reassign.isPending) return;
    onOpenChange(false);
    setTarget("");
    reassign.reset();
  };

  const move = async () => {
    if (!target || reassign.isPending) return;
    try {
      await reassign.mutateAsync({ issueId: item.issue.id, unitId: target });
      onOpenChange(false);
      setTarget("");
    } catch {
      // The error stays in the dialog so the requester can pick another target.
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) onOpenChange(true);
        else close();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.reassign.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.reassign.description)}
          </DialogDescription>
        </DialogHeader>
        <div
          role="group"
          aria-label={t(($) => $.reassign.target_label)}
          className="flex max-h-72 flex-col gap-1 overflow-y-auto"
        >
          {options.length === 0 ? (
            <p className="px-1 py-3 text-caption text-muted-foreground">
              {t(($) => $.reassign.empty)}
            </p>
          ) : null}
          {options.map((option) => {
            const selected = option.id === target;
            const unavailable = option.count === 0;
            return (
              <button
                key={option.id}
                type="button"
                aria-pressed={selected}
                disabled={unavailable || reassign.isPending}
                onClick={() => setTarget(option.id)}
                className={cn(
                  "flex w-full items-center justify-between gap-3 rounded-md border px-3 py-2 text-left transition-colors",
                  "hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50",
                  selected && "border-brand bg-muted",
                )}
              >
                <span className="min-w-0 truncate text-body">{option.path}</span>
                <span className="shrink-0 text-caption text-muted-foreground">
                  {unavailable
                    ? t(($) => $.reassign.no_agents)
                    : t(($) => $.candidates.agents, { count: option.count })}
                </span>
              </button>
            );
          })}
        </div>
        {reassign.error ? (
          <p role="alert" className="text-caption text-destructive">
            <DispatchErrorText error={reassign.error} />
          </p>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={close} disabled={reassign.isPending}>
            {t(($) => $.reassign.cancel)}
          </Button>
          <Button onClick={() => void move()} disabled={!target || reassign.isPending}>
            {t(($) => $.reassign.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
