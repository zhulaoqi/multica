"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  Building2,
  CircleAlert,
  CircleCheck,
  CircleDot,
  ListChecks,
  UserRound,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { statusCategoryOfKey } from "@multica/core/issues";
import { childIssuesOptions, issueTasksOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AgentTask, Issue } from "@multica/core/types";
import type { DispatchListItem } from "@multica/core/workbench";
import { useT, useTimeAgo } from "../i18n";
import { useStatusLabel } from "../issues/utils/status-label";
import { DispatchDeliverables } from "./dispatch-deliverables";
import { DispatchResultBlock } from "./dispatch-result-block";
import type { ChildProgress, DispatchPhase } from "./use-workbench-conversation";

/**
 * The collaboration trail the requester follows from the C-end: submitted to
 * a department, routed, decomposed by its lead, worked by named agents, then
 * delivered with its result and files, and finally accepted. It reads only
 * what the requester may see — the dispatch root and the tree below it.
 *
 * Deliberately high level: issue titles, agent names, statuses and times. No
 * comments, tool calls, or prompts — those stay inside the department.
 */
export function DispatchTrail({
  wsId,
  item,
  phase,
  progress,
}: {
  wsId: string;
  item: DispatchListItem;
  phase: DispatchPhase;
  progress?: ChildProgress;
}) {
  const { t } = useT("workbench");
  const timeAgo = useTimeAgo();
  const statusLabel = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const { data: children = [] } = useQuery(childIssuesOptions(wsId, item.issue.id));

  // One task-runs request per decomposed issue. Bounded so a wide plan cannot
  // turn the trail into an unbounded fan-out.
  const taskQueries = useQueries({
    queries: children.slice(0, 20).map((child) => ({
      ...issueTasksOptions(child.id),
      retry: false,
    })),
    combine: (results) => {
      const byIssue = new Map<string, AgentTask[]>();
      results.forEach((result, index) => {
        const child = children[index];
        if (child && result.data) byIssue.set(child.id, result.data);
      });
      return byIssue;
    },
  });

  // Each decomposed task settles on its latest run, which is the one that owns
  // its current status and timestamps.
  const rows = useMemo(
    () =>
      children.map((child) => {
        const runs = taskQueries.get(child.id) ?? [];
        const latest = [...runs].sort(
          (a, b) => Date.parse(b.dispatched_at ?? "") - Date.parse(a.dispatched_at ?? ""),
        )[0];
        return { child, task: latest };
      }),
    [children, taskQueries],
  );

  const done = progress?.done ?? 0;
  const total = progress?.total ?? children.length;
  const delivered = phase === "completed" || phase === "in_review";
  const accepted = phase === "completed";
  const failed = phase === "failed";
  const cancelled = phase === "cancelled";

  return (
    <section
      aria-label={t(($) => $.trail.title)}
      className="flex flex-col gap-3 rounded-xl border bg-card p-4"
    >
      <h3 className="text-caption font-medium text-muted-foreground">
        {t(($) => $.trail.title)}
      </h3>
      <ol className="flex flex-col gap-3">
        <TrailStep
          icon={<CircleDot className="size-4 text-muted-foreground" aria-hidden />}
          title={
            item.personal
              ? t(($) => $.trail.submitted_personal)
              : t(($) => $.trail.submitted, {
                  unit: item.unit_path || t(($) => $.page.unknown_department),
                })
          }
          meta={timeAgo(item.dispatched_at)}
        />
        <TrailStep
          icon={
            failed || cancelled ? (
              <CircleAlert className="size-4 text-destructive" aria-hidden />
            ) : (
              <ListChecks className="size-4 text-muted-foreground" aria-hidden />
            )
          }
          title={
            failed || cancelled
              ? t(($) => $.trail.stopped)
              : children.length > 0
                ? t(($) => $.trail.decomposed, { count: children.length })
                : t(($) => $.trail.decomposing)
          }
          meta={
            total > 0
              ? t(($) => $.status.progress, { done, total })
              : undefined
          }
        >
          {rows.length > 0 ? (
            <ul className="flex flex-col gap-1.5">
              {rows.map(({ child, task }) => (
                <TrailRow
                  key={child.id}
                  child={child}
                  task={task}
                  statusLabel={statusLabel}
                  agentName={
                    child.assignee_type && child.assignee_id
                      ? getActorName(child.assignee_type, child.assignee_id)
                      : null
                  }
                  timeAgo={timeAgo}
                />
              ))}
            </ul>
          ) : null}
        </TrailStep>
        {delivered ? (
          <TrailStep
            icon={<CircleCheck className="size-4 text-warning" aria-hidden />}
            title={t(($) => $.trail.delivered)}
          >
            <DispatchResultBlock issueId={item.issue.id} />
            <DispatchDeliverables wsId={wsId} issueId={item.issue.id} />
          </TrailStep>
        ) : null}
        <TrailStep
          icon={
            accepted ? (
              <CircleCheck className="size-4 text-success" aria-hidden />
            ) : (
              <CircleDot className="size-4 text-muted-foreground" aria-hidden />
            )
          }
          title={accepted ? t(($) => $.trail.accepted) : t(($) => $.trail.acceptance)}
          meta={
            accepted || delivered
              ? undefined
              : failed || cancelled
                ? undefined
                : t(($) => $.trail.acceptance_waiting)
          }
        />
      </ol>
      {item.unit_path ? (
        <p className="flex items-center gap-1 text-caption text-muted-foreground">
          <Building2 className="size-3.5 shrink-0" aria-hidden />
          <span className="truncate">{item.unit_path}</span>
        </p>
      ) : item.personal ? (
        <p className="flex items-center gap-1 text-caption text-muted-foreground">
          <UserRound className="size-3.5 shrink-0" aria-hidden />
          <span className="truncate">{t(($) => $.page.personal_squad)}</span>
        </p>
      ) : null}
    </section>
  );
}

function TrailStep({
  icon,
  title,
  meta,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  meta?: string;
  children?: React.ReactNode;
}) {
  return (
    <li className="flex gap-2.5">
      <span className="mt-0.5 shrink-0">{icon}</span>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <p className="flex flex-wrap items-baseline gap-x-2 text-body">
          <span className="min-w-0">{title}</span>
          {meta ? (
            <span className="text-caption text-muted-foreground">{meta}</span>
          ) : null}
        </p>
        {children}
      </div>
    </li>
  );
}

function RowDot({ status, category }: { status: string; category: string }) {
  const isRunning =
    category === "started" || status === "in_progress" || status === "in_review";
  if (isRunning) return <Spinner className="size-3 text-muted-foreground" />;
  return (
    <span
      aria-hidden
      className={cn(
        "size-1.5 shrink-0 rounded-full",
        status === "in_review"
          ? "bg-warning"
          : status === "blocked"
            ? "bg-destructive"
            : category === "done" || category === "closed"
              ? "bg-success"
              : "bg-muted-foreground/50",
      )}
    />
  );
}

function TrailRow({
  child,
  task,
  statusLabel,
  agentName,
  timeAgo,
}: {
  child: Issue;
  task: AgentTask | undefined;
  statusLabel: (status: string) => string;
  agentName: string | null;
  timeAgo: (iso: string) => string;
}) {
  const category = child.status_category ?? statusCategoryOfKey(child.status);
  const finished = task?.completed_at;
  const started = task?.started_at ?? task?.dispatched_at ?? null;
  const timing =
    finished && started
      ? `${timeAgo(started)} → ${timeAgo(finished)}`
      : started
        ? timeAgo(started)
        : timeAgo(child.created_at);
  return (
    <li className="flex min-w-0 items-center gap-2 text-caption">
      <RowDot status={child.status} category={category} />
      <span className="min-w-0 flex-1 truncate text-foreground">{child.title}</span>
      {agentName ? (
        <span className="max-w-28 shrink-0 truncate text-muted-foreground">{agentName}</span>
      ) : null}
      <span className="shrink-0 text-muted-foreground">{statusLabel(child.status)}</span>
      <span className="shrink-0 text-muted-foreground">{timing}</span>
    </li>
  );
}
