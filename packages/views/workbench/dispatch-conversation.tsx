"use client";

import { useState } from "react";
import {
  ArrowLeftRight,
  Building2,
  FolderKanban,
  CircleAlert,
  CircleCheck,
  CircleDot,
  RotateCcw,
  UserRound,
  XCircle,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Progress } from "@multica/ui/components/ui/progress";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { errorCode } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DispatchCandidate, DispatchListItem } from "@multica/core/workbench";
import { useHomeMode } from "../common/home-mode";
import { useT, useTimeAgo } from "../i18n";
import { AppLink } from "../navigation";
import { canReview } from "./dispatch-actions";
import { DispatchDeliverables } from "./dispatch-deliverables";
import { DispatchPlan } from "./dispatch-plan";
import { DispatchResultBlock } from "./dispatch-result-block";
import { DispatchTrail } from "./dispatch-trail";
import { ReassignDispatchDialog } from "./reassign-dispatch";
import { DispatchReview } from "./dispatch-review";
import type { ChildProgress, DispatchPhase } from "./use-workbench-conversation";

/** A submission that has not been accepted by the server yet. */
export interface WorkbenchAttempt {
  id: string;
  text: string;
  status: "pending" | "failed";
  error?: unknown;
  candidates: DispatchCandidate[];
  /** Uploaded files to send again with a retry or a picked department. */
  attachmentIds: string[];
  /** The attempt targeted the requester's personal squad, not a department. */
  personal?: boolean;
  /** The project the attempt was handed through, when it named one. */
  project?: { id: string; name: string };
}

function UserBubble({ text }: { text: string }) {
  return (
    <p className="ml-auto max-w-[85%] rounded-2xl rounded-br-md bg-muted px-4 py-2.5 text-body whitespace-pre-wrap break-words">
      {text}
    </p>
  );
}

export function DispatchErrorText({ error }: { error: unknown }) {
  const { t } = useT("workbench");
  switch (errorCode(error)) {
    case "dispatch_text_required":
      return <>{t(($) => $.errors.dispatch_text_required)}</>;
    case "dispatch_text_too_long":
      return <>{t(($) => $.errors.dispatch_text_too_long)}</>;
    case "org_dispatch_unit_not_found":
      return <>{t(($) => $.errors.org_dispatch_unit_not_found)}</>;
    case "org_dispatch_forbidden":
      return <>{t(($) => $.errors.org_dispatch_forbidden)}</>;
    case "agent_binding_required":
      return <>{t(($) => $.errors.agent_binding_required)}</>;
    case "org_dispatch_no_agents":
      return <>{t(($) => $.errors.org_dispatch_no_agents)}</>;
    case "org_dispatch_leader_unavailable":
      return <>{t(($) => $.errors.org_dispatch_leader_unavailable)}</>;
    case "org_dispatch_leader_offline":
      return <>{t(($) => $.errors.org_dispatch_leader_offline)}</>;
    case "org_dispatch_unroutable":
      return <>{t(($) => $.errors.org_dispatch_unroutable)}</>;
    case "org_dispatch_failed":
      return <>{t(($) => $.errors.org_dispatch_failed)}</>;
    case "personal_squad_not_found":
      return <>{t(($) => $.errors.personal_squad_not_found)}</>;
    case "org_dispatch_target_conflict":
      return <>{t(($) => $.errors.org_dispatch_target_conflict)}</>;
    case "org_dispatch_reassign_forbidden":
      return <>{t(($) => $.errors.org_dispatch_reassign_forbidden)}</>;
    case "dispatch_not_found":
      return <>{t(($) => $.errors.dispatch_not_found)}</>;
    case "dispatch_organization_only":
      return <>{t(($) => $.errors.dispatch_organization_only)}</>;
    default:
      // Unknown codes include a 422 whose body carried candidates but no
      // recognizable code; the candidates paragraph explains it better.
      return <>{t(($) => $.errors.generic)}</>;
  }
}

function phaseIcon(phase: DispatchPhase) {
  switch (phase) {
    case "completed":
      return <CircleCheck className="size-4 text-success" aria-hidden />;
    case "in_review":
      return <CircleCheck className="size-4 text-warning" aria-hidden />;
    case "failed":
      return <CircleAlert className="size-4 text-destructive" aria-hidden />;
    case "cancelled":
      return <XCircle className="size-4 text-muted-foreground" aria-hidden />;
    case "queued":
      return <CircleDot className="size-4 text-muted-foreground" aria-hidden />;
    default:
      return <Spinner className="size-4 text-muted-foreground" />;
  }
}

function PhaseMessage({
  phase,
  personal,
}: {
  phase: DispatchPhase;
  personal?: boolean;
}) {
  const { t } = useT("workbench");
  switch (phase) {
    case "progress":
      return null;
    case "queued":
      return <>{personal ? t(($) => $.status.queued_personal) : t(($) => $.status.queued)}</>;
    case "decomposing":
      return (
        <>{personal ? t(($) => $.status.decomposing_personal) : t(($) => $.status.decomposing)}</>
      );
    case "in_review":
      return <>{t(($) => $.status.in_review)}</>;
    case "completed":
      return <>{t(($) => $.status.completed)}</>;
    case "failed":
      return <>{personal ? t(($) => $.status.failed_personal) : t(($) => $.status.failed)}</>;
    case "cancelled":
      return <>{t(($) => $.status.cancelled)}</>;
  }
}

/**
 * One dispatch as a conversation turn: the requester's words, then the
 * department's current state — routed, decomposing, progress, review, or
 * receipt. The department path and issue link stay visible in every phase so
 * the requester can follow the work in the issue view at any time.
 */
export function DispatchConversation({
  wsId,
  item,
  phase,
  progress,
  onRetry,
}: {
  wsId: string;
  item: DispatchListItem;
  phase: DispatchPhase;
  progress?: ChildProgress;
  onRetry?: () => void;
}) {
  const { t } = useT("workbench");
  const p = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const homeMode = useHomeMode();
  const [reassignOpen, setReassignOpen] = useState(false);
  const issueHref = p.issueDetail(item.issue.id);
  const percent =
    progress && progress.total > 0
      ? Math.round((progress.done / progress.total) * 100)
      : 0;

  return (
    <div className="flex flex-col gap-3">
      <UserBubble text={item.issue.description ?? item.issue.title} />
      <article className="flex gap-3 rounded-xl border bg-card p-4">
        <span className="mt-0.5 shrink-0">{phaseIcon(phase)}</span>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          {phase === "progress" && progress ? (
            <p className="text-body font-medium">
              {t(($) => $.status.progress, {
                done: progress.done,
                total: progress.total,
              })}
            </p>
          ) : (
            <p className="text-body">
              <PhaseMessage phase={phase} personal={item.personal} />
            </p>
          )}
          {phase === "progress" && progress ? (
            <Progress value={percent} className="w-full" aria-label={t(($) => $.status.progress, {
              done: progress.done,
              total: progress.total,
            })} />
          ) : null}
          {homeMode ? (
            // The C-end follows the whole journey: who decomposed the work,
            // which agent did what and when, and what came back.
            <DispatchTrail wsId={wsId} item={item} phase={phase} progress={progress} />
          ) : (
            <>
              {progress && progress.total > 0 ? (
                <DispatchPlan wsId={wsId} issueId={item.issue.id} progress={progress} />
              ) : null}
              {phase === "completed" || phase === "in_review" ? (
                <DispatchResultBlock issueId={item.issue.id} />
              ) : null}
              {phase === "completed" || phase === "in_review" ? (
                <DispatchDeliverables wsId={wsId} issueId={item.issue.id} />
              ) : null}
            </>
          )}
          {canReview(phase) ? <DispatchReview item={item} /> : null}
          {phase === "failed" ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.card.failure_hint)}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
            {item.unit_path ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <Building2 className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{item.unit_path}</span>
              </span>
            ) : item.personal ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <UserRound className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{t(($) => $.page.personal_squad)}</span>
              </span>
            ) : null}
            <span>{t(($) => $.status.dispatched_at, { time: timeAgo(item.dispatched_at) })}</span>
            {homeMode ? (
              <span className="font-medium text-foreground">{item.issue.identifier}</span>
            ) : (
              <AppLink
                href={issueHref}
                className="font-medium text-foreground underline-offset-2 hover:underline"
              >
                {item.issue.identifier}
              </AppLink>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {!homeMode && (
              <Button
                variant="outline"
                size="sm"
                nativeButton={false}
                render={<AppLink href={issueHref} />}
              >
                {t(($) => $.card.view_issue)}
              </Button>
            )}
            {phase === "failed" && onRetry ? (
              <Button variant="outline" size="sm" onClick={onRetry}>
                <RotateCcw />
                {t(($) => $.card.retry)}
              </Button>
            ) : null}
            {!item.personal && phase !== "completed" && phase !== "cancelled" ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setReassignOpen(true)}
              >
                <ArrowLeftRight />
                {t(($) => $.reassign.action)}
              </Button>
            ) : null}
          </div>
          {!item.personal && phase !== "completed" && phase !== "cancelled" ? (
            <ReassignDispatchDialog
              wsId={wsId}
              item={item}
              open={reassignOpen}
              onOpenChange={setReassignOpen}
            />
          ) : null}
        </div>
      </article>
    </div>
  );
}

/**
 * A dispatch that has not reached the server yet, or one the server rejected.
 * Candidates from an ambiguous route are clickable: picking one resends the
 * same text with an explicit department.
 */
export function DispatchAttemptConversation({
  attempt,
  onRetry,
  onPickCandidate,
}: {
  attempt: WorkbenchAttempt;
  onRetry: () => void;
  onPickCandidate: (candidate: DispatchCandidate) => void;
}) {
  const { t } = useT("workbench");
  return (
    <div className="flex flex-col gap-3">
      <UserBubble text={attempt.text} />
      <article className="flex gap-3 rounded-xl border bg-card p-4">
        <span className="mt-0.5 shrink-0">
          {attempt.status === "pending" ? (
            <Spinner className="size-4 text-muted-foreground" />
          ) : (
            <CircleAlert className="size-4 text-destructive" aria-hidden />
          )}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          {attempt.project ? (
            <span className="inline-flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
              <FolderKanban className="size-3.5 shrink-0" aria-hidden />
              <span className="truncate">{attempt.project.name}</span>
            </span>
          ) : null}
          {attempt.status === "pending" ? (
            <p className="text-body">{t(($) => $.page.routing)}</p>
          ) : (
            <>
              {attempt.candidates.length > 0 ? (
                <p className="text-body">{t(($) => $.errors.ambiguous)}</p>
              ) : (
                <p className="text-body">
                  <DispatchErrorText error={attempt.error} />
                </p>
              )}
              {attempt.candidates.length > 0 ? (
                <div className="flex flex-col gap-1.5">
                  <p className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.candidates.title)}
                  </p>
                  <div className="flex flex-wrap gap-1.5">
                    {attempt.candidates.map((candidate) => (
                      <Button
                        key={candidate.unit_id}
                        variant="outline"
                        size="sm"
                        onClick={() => onPickCandidate(candidate)}
                      >
                        <span className="truncate">
                          {candidate.path || t(($) => $.page.unknown_department)}
                        </span>
                        <span className="text-caption text-muted-foreground">
                          {t(($) => $.candidates.agents, {
                            count: candidate.agent_count,
                          })}
                        </span>
                      </Button>
                    ))}
                  </div>
                </div>
              ) : null}
              <div>
                <Button variant="outline" size="sm" onClick={onRetry}>
                  <RotateCcw />
                  {t(($) => $.card.retry)}
                </Button>
              </div>
            </>
          )}
        </div>
      </article>
    </div>
  );
}
