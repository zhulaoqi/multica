"use client";

import { useMemo, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useMyDispatches, type DispatchListItem } from "@multica/core/workbench";
import { useT, useTimeAgo } from "../i18n";
import { dispatchPhase } from "../workbench/use-workbench-conversation";
import {
  FILTERS,
  matchesFilter,
  phaseDotClass,
  type DispatchFilter,
} from "../workbench/workbench-page";

/**
 * The C-end's task list: the work this person sent to a department or their
 * own squad, and where it stands. Opening a row switches to the conversation
 * tab with that thread selected; no issue page, no admin chrome.
 */
export function HomeTasksPage({ onOpen }: { onOpen: (issueId: string) => void }) {
  const { t } = useT("workbench");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { dispatches, isLoading, error } = useMyDispatches(wsId);
  const [filter, setFilter] = useState<DispatchFilter>("all");

  const phaseFor = (item: DispatchListItem) => dispatchPhase(item, undefined);
  const counts = useMemo<Record<DispatchFilter, number>>(() => {
    const next: Record<DispatchFilter, number> = {
      all: dispatches.length,
      review: 0,
      active: 0,
      completed: 0,
    };
    for (const item of dispatches) {
      const phase = dispatchPhase(item, undefined);
      for (const key of ["review", "active", "completed"] as const) {
        if (matchesFilter(phase, key)) next[key] += 1;
      }
    }
    return next;
  }, [dispatches]);
  const visible = dispatches.filter((item) => matchesFilter(phaseFor(item), filter));

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 py-6">
        <h1 className="text-title font-medium">{t(($) => $.home.tasks)}</h1>
        <div role="group" aria-label={t(($) => $.page.filters.label)} className="flex flex-wrap gap-1">
          {FILTERS.map((key) => (
            <Button
              key={key}
              variant={filter === key ? "secondary" : "ghost"}
              size="sm"
              aria-pressed={filter === key}
              className="h-6 gap-1 rounded-full px-2 text-caption"
              onClick={() => setFilter(key)}
            >
              {t(($) => $.page.filters[key])}
              <span className="tabular-nums text-muted-foreground">{counts[key]}</span>
            </Button>
          ))}
        </div>
        {isLoading && dispatches.length === 0 ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </div>
        ) : error ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.load_error)}</p>
        ) : dispatches.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.empty_history)}</p>
        ) : visible.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.filters.empty)}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {visible.map((item) => (
              <li key={item.issue.id}>
                <button
                  type="button"
                  onClick={() => onOpen(item.issue.id)}
                  className="flex w-full flex-col gap-1 rounded-lg border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted"
                >
                  <span className="flex items-center gap-2">
                    <span
                      aria-hidden
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        phaseDotClass(phaseFor(item)),
                      )}
                    />
                    <span className="min-w-0 flex-1 truncate text-body">
                      {item.issue.title}
                    </span>
                  </span>
                  <span className="min-w-0 truncate pl-3.5 text-caption text-muted-foreground">
                    {item.personal
                      ? t(($) => $.page.personal_squad)
                      : item.unit_path || t(($) => $.page.unknown_department)}
                    {" · "}
                    {timeAgo(item.dispatched_at)}
                  </span>
                  {item.blocker?.missing ? (
                    <span className="min-w-0 truncate pl-3.5 text-caption text-warning">
                      {t(($) => $.page.blocked, { missing: item.blocker.missing })}
                    </span>
                  ) : null}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
