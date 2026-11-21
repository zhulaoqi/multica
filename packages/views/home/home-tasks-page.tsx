"use client";

import { useMemo } from "react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { statusCategoryOfKey } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import type { DispatchListItem } from "@multica/core/workbench";
import { useT, useTimeAgo } from "../i18n";
import { dispatchPhase, type DispatchPhase } from "../workbench/use-workbench-conversation";
import { useMyDispatchScope } from "../workbench/use-my-dispatch-scope";
import { phaseDotClass } from "../workbench/workbench-page";

type BoardColumn = "active" | "review" | "done";

const BOARD_COLUMNS: BoardColumn[] = ["active", "review", "done"];

function columnFor(phase: DispatchPhase): BoardColumn {
  if (phase === "in_review") return "review";
  if (phase === "completed" || phase === "failed" || phase === "cancelled") {
    return "done";
  }
  return "active";
}

interface BoardCard {
  item: DispatchListItem;
  phase: DispatchPhase;
  done: number;
  total: number;
  /** Names of the agents and squads the lead handed parts of this work to. */
  agents: string[];
  /** Agents with a run on this work right now. */
  working: number;
}

/**
 * The C-end's task board: the work this person sent, in three lanes — moving,
 * waiting for their review, and settled. Each card names the department, how
 * far the lead's plan has got, and which agents are on it, so the requester
 * learns where their work stands without opening the admin side.
 */
export function HomeTasksPage({ onOpen }: { onOpen: (issueId: string) => void }) {
  const { t } = useT("workbench");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const {
    dispatches,
    isLoading,
    error,
    childrenByRoot,
    progressByRoot,
    tasksByIssue,
  } = useMyDispatchScope(wsId);

  const board = useMemo(() => {
    const columns: Record<BoardColumn, BoardCard[]> = {
      active: [],
      review: [],
      done: [],
    };
    for (const item of dispatches) {
      const progress = progressByRoot.get(item.issue.id);
      const phase = dispatchPhase(item, progress);
      const children = childrenByRoot.get(item.issue.id) ?? [];
      const names = new Set<string>();
      for (const child of children) {
        if (child.assignee_type && child.assignee_id) {
          names.add(getActorName(child.assignee_type, child.assignee_id));
        }
      }
      if (names.size === 0 && item.issue.assignee_type && item.issue.assignee_id) {
        names.add(getActorName(item.issue.assignee_type, item.issue.assignee_id));
      }
      const working = new Set<string>();
      const issueIds = [item.issue.id, ...children.map((child) => child.id)];
      for (const issueId of issueIds) {
        for (const task of tasksByIssue.get(issueId) ?? []) {
          if (task.status === "running") working.add(task.agent_id);
        }
      }
      columns[columnFor(phase)].push({
        item,
        phase,
        done: progress?.done ?? children.filter(isDone).length,
        total: progress?.total ?? children.length,
        agents: [...names],
        working: working.size,
      });
    }
    return columns;
  }, [childrenByRoot, dispatches, getActorName, progressByRoot, tasksByIssue]);

  const nothingSent = dispatches.length === 0;

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-6">
        <h1 className="text-title font-medium">{t(($) => $.home.tasks)}</h1>
        {isLoading && nothingSent ? (
          <div className="grid gap-3 md:grid-cols-3">
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
          </div>
        ) : error ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.load_error)}</p>
        ) : nothingSent ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.empty_history)}</p>
        ) : (
          <div className="grid gap-3 md:grid-cols-3">
            {BOARD_COLUMNS.map((column) => (
              <section
                key={column}
                aria-label={t(($) => $.board.columns[column])}
                className="flex min-w-0 flex-col gap-2 rounded-lg bg-muted/30 p-2"
              >
                <h2 className="flex items-center gap-2 px-1 text-caption font-medium text-muted-foreground">
                  <span className="min-w-0 flex-1 truncate">
                    {t(($) => $.board.columns[column])}
                  </span>
                  <span className="tabular-nums">{board[column].length}</span>
                </h2>
                {board[column].length === 0 ? (
                  <p className="px-1 text-caption text-muted-foreground">
                    {t(($) => $.board.empty_column)}
                  </p>
                ) : (
                  <ul className="flex flex-col gap-1.5">
                    {board[column].map((card) => (
                      <li key={card.item.issue.id}>
                        <BoardCard
                          card={card}
                          onOpen={onOpen}
                          timeAgo={timeAgo}
                        />
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function isDone(issue: Issue): boolean {
  const category = issue.status_category ?? statusCategoryOfKey(issue.status);
  return category === "done" || category === "closed";
}

function BoardCard({
  card,
  onOpen,
  timeAgo,
}: {
  card: BoardCard;
  onOpen: (issueId: string) => void;
  timeAgo: (iso: string) => string;
}) {
  const { t } = useT("workbench");
  const { item, phase, done, total, agents, working } = card;
  const percent = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <button
      type="button"
      onClick={() => onOpen(item.issue.id)}
      className="flex w-full flex-col gap-1.5 rounded-lg border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted"
    >
      <span className="flex items-center gap-2">
        <span
          aria-hidden
          className={cn("size-1.5 shrink-0 rounded-full", phaseDotClass(phase))}
        />
        <span className="min-w-0 flex-1 truncate text-body">{item.issue.title}</span>
      </span>
      <span className="flex min-w-0 items-center gap-2 pl-3.5 text-caption text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">
          {item.personal
            ? t(($) => $.page.personal_squad)
            : item.unit_path || t(($) => $.page.unknown_department)}
        </span>
        <span className="shrink-0">{timeAgo(item.dispatched_at)}</span>
      </span>
      {total > 0 ? (
        <span className="flex items-center gap-2 pl-3.5">
          <span className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
            <span
              className="block h-full rounded-full bg-brand"
              style={{ width: `${percent}%` }}
            />
          </span>
          <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
            {t(($) => $.status.progress, { done, total })}
          </span>
        </span>
      ) : null}
      {agents.length > 0 || working > 0 ? (
        <span className="flex min-w-0 items-center gap-1.5 pl-3.5 text-caption text-muted-foreground">
          {working > 0 ? <Spinner className="size-3 shrink-0" /> : null}
          <span className="min-w-0 truncate">
            {agents.length > 0
              ? agents.slice(0, 3).join(", ") +
                (agents.length > 3 ? ` +${agents.length - 3}` : "")
              : t(($) => $.board.agent_working, { count: working })}
          </span>
        </span>
      ) : null}
      {item.blocker?.missing ? (
        <span className="min-w-0 truncate pl-3.5 text-caption text-warning">
          {t(($) => $.page.blocked, { missing: item.blocker.missing })}
        </span>
      ) : null}
    </button>
  );
}
