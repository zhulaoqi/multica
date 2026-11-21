"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ListChecks } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { childIssuesOptions } from "@multica/core/issues/queries";
import { statusCategoryOfKey } from "@multica/core/issues";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Issue, IssueStatusCategory } from "@multica/core/types";
import { useHomeMode } from "../common/home-mode";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { useStatusLabel } from "../issues/utils/status-label";
import type { ChildProgress } from "./use-workbench-conversation";

function dotClass(status: string, category: IssueStatusCategory): string {
  if (status === "in_review") return "bg-warning";
  if (status === "blocked") return "bg-destructive";
  switch (category) {
    case "done":
      return "bg-success";
    case "closed":
      return "bg-success/60";
    case "started":
      return "bg-brand";
    default:
      return "bg-muted-foreground/50";
  }
}

/**
 * The department's plan as it stands: the sub-issues the leader created, each
 * with its owner and current status. It reads the same issue queries the
 * issue view uses, so a task ticked off there ticks off here too.
 *
 * Collapsed to a one-line summary once the plan grows; the requester reading
 * a long thread should not scroll past every task to reach the next message.
 */
export function DispatchPlan({
  wsId,
  issueId,
  progress,
}: {
  wsId: string;
  issueId: string;
  progress?: ChildProgress;
}) {
  const { t } = useT("workbench");
  const paths = useWorkspacePaths();
  const homeMode = useHomeMode();
  const statusLabel = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const [open, setOpen] = useState(false);

  const { data: children = [] } = useQuery(childIssuesOptions(wsId, issueId));
  if (children.length === 0) return null;

  const done = progress?.done ?? children.filter((child) => {
    const category = child.status_category ?? statusCategoryOfKey(child.status);
    return category === "done" || category === "closed";
  }).length;
  const total = progress?.total ?? children.length;
  const collapsible = children.length > 4;

  return (
    <section
      aria-label={t(($) => $.plan.title)}
      className="flex flex-col gap-1.5 rounded-lg border bg-muted/30 px-3 py-2.5"
    >
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left"
        onClick={() => collapsible && setOpen((value) => !value)}
        aria-expanded={collapsible ? open : true}
        disabled={!collapsible}
      >
        <ListChecks className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 text-caption font-medium text-muted-foreground">
          {t(($) => $.plan.title)}
        </span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.status.progress, { done, total })}
        </span>
        {collapsible ? (
          <ChevronDown
            className={cn(
              "size-3.5 shrink-0 text-muted-foreground transition-transform",
              open && "rotate-180",
            )}
            aria-hidden
          />
        ) : null}
      </button>
      {!collapsible || open ? (
        <ul className="flex flex-col gap-0.5">
          {children.map((child) => (
            <PlanRow
              key={child.id}
              issue={child}
              href={homeMode ? null : paths.issueDetail(child.id)}
              statusLabel={statusLabel}
              assigneeName={
                child.assignee_type && child.assignee_id
                  ? getActorName(child.assignee_type, child.assignee_id)
                  : null
              }
            />
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function PlanRow({
  issue,
  href,
  statusLabel,
  assigneeName,
}: {
  issue: Issue;
  /** Null on the C-end, where issue pages belong to the admin side. */
  href: string | null;
  statusLabel: (status: string) => string;
  assigneeName: string | null;
}) {
  const category = issue.status_category ?? statusCategoryOfKey(issue.status);
  const titleClass = "min-w-0 flex-1 truncate text-foreground";
  return (
    <li className="flex min-w-0 items-center gap-2 text-caption">
      <span
        aria-hidden
        className={cn("size-1.5 shrink-0 rounded-full", dotClass(issue.status, category))}
      />
      {href ? (
        <AppLink
          href={href}
          className={cn(titleClass, "underline-offset-2 hover:underline")}
        >
          {issue.title}
        </AppLink>
      ) : (
        <span className={titleClass}>{issue.title}</span>
      )}
      <span className="shrink-0 text-muted-foreground">{statusLabel(issue.status)}</span>
      {assigneeName ? (
        <span className="max-w-32 shrink-0 truncate text-muted-foreground">{assigneeName}</span>
      ) : null}
    </li>
  );
}
