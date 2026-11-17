"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ListChecks,
  RotateCcw,
  TriangleAlert,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useCreateComment, useUpdateIssue } from "@multica/core/issues/mutations";
import type { DispatchListItem } from "@multica/core/workbench";
import { workbenchKeys } from "@multica/core/workbench/queries";
import { useT } from "../../i18n";
import {
  rejectionComment,
  subjectMention,
} from "../../workbench/dispatch-actions";
import type { WeeklyProgress } from "../game/progress";

export interface ReceptionAgent {
  id: string;
  name: string;
}

export interface OfficeReceptionPanelProps {
  wsId: string;
  todayCompleted: number;
  weekly: WeeklyProgress;
  companyLevel: number;
  mvpName: string | null;
  /** Delivered dispatches waiting on the requester's verdict. */
  waiting: DispatchListItem[];
  /** Agents whose runtime is offline or whose queue is backing up. */
  stuck: ReceptionAgent[];
  onClose: () => void;
  /** The requester accepted a result; the scene stamps it as archived. */
  onAccepted?: () => void;
  className?: string;
}

/**
 * The front desk's daily report. Standing at the desk and pressing T opens
 * it: what the office landed today and this week, who is waiting on a
 * verdict, and who needs a look. The report is read-only except for
 * sign-off, which uses the same issue mutations as the workbench.
 */
export function OfficeReceptionPanel({
  wsId,
  todayCompleted,
  weekly,
  companyLevel,
  mvpName,
  waiting,
  stuck,
  onClose,
  onAccepted,
  className,
}: OfficeReceptionPanelProps) {
  const { t } = useT("office");
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    panelRef.current?.focus();
  }, []);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      event.preventDefault();
      onClose();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  const percent =
    weekly.goal > 0 ? Math.min(100, Math.round((weekly.completed / weekly.goal) * 100)) : 0;

  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-labelledby={titleId}
      tabIndex={-1}
      className={cn(
        "pointer-events-auto flex max-h-[calc(100%-1.5rem)] w-80 max-w-[calc(100%-1.5rem)] flex-col gap-3 overflow-y-auto rounded-xl border border-border/60 bg-surface-raised p-3 text-caption shadow-lg outline-none",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <ListChecks className="size-4 shrink-0 text-muted-foreground" />
        <p id={titleId} className="min-w-0 flex-1 truncate text-body font-medium">
          {t(($) => $.page.reception.title)}
        </p>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.page.reception.close)}
          onClick={onClose}
        >
          <X />
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-0.5 rounded-lg bg-muted/50 px-2 py-1.5">
          <span className="text-micro text-muted-foreground">
            {t(($) => $.page.reception.today)}
          </span>
          <span className="text-body font-medium tabular-nums">
            {t(($) => $.page.reception.today_value, { count: todayCompleted })}
          </span>
        </div>
        <div className="flex flex-col gap-0.5 rounded-lg bg-muted/50 px-2 py-1.5">
          <span className="text-micro text-muted-foreground">
            {t(($) => $.page.reception.company_level, { level: companyLevel })}
          </span>
          <span className="text-body font-medium tabular-nums">
            {mvpName
              ? t(($) => $.page.reception.mvp, { name: mvpName })
              : t(($) => $.page.reception.mvp_empty)}
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-micro text-muted-foreground">
            {t(($) => $.page.reception.week)}
          </span>
          <span className="tabular-nums">
            {t(($) => $.page.reception.week_value, {
              done: weekly.completed,
              goal: weekly.goal,
            })}
          </span>
        </div>
        <div
          className="h-1.5 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={weekly.goal}
          aria-valuenow={weekly.completed}
          aria-label={t(($) => $.page.reception.week)}
        >
          <div
            className={cn("h-full rounded-full", weekly.met ? "bg-warning" : "bg-brand")}
            style={{ width: `${percent}%` }}
          />
        </div>
        {weekly.met && (
          <span className="text-micro text-warning">
            {t(($) => $.page.reception.week_met)}
          </span>
        )}
      </div>

      <section className="flex flex-col gap-2">
        <h3 className="text-micro font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.page.reception.waiting)}
        </h3>
        {waiting.length === 0 ? (
          <p className="text-muted-foreground">
            {t(($) => $.page.reception.waiting_empty)}
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {waiting.map((item) => (
              <ReceptionReviewRow
                key={item.issue.id}
                wsId={wsId}
                item={item}
                onAccepted={onAccepted}
              />
            ))}
          </ul>
        )}
      </section>

      {stuck.length > 0 && (
        <section className="flex flex-col gap-1">
          <h3 className="flex items-center gap-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">
            <TriangleAlert className="size-3 text-warning" />
            {t(($) => $.page.reception.stuck, { count: stuck.length })}
          </h3>
          <ul className="flex flex-col">
            {stuck.map((agent) => (
              <li key={agent.id} className="truncate text-muted-foreground">
                {agent.name}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/**
 * One delivered result waiting on the requester. Accepting closes the root
 * issue; sending it back posts the reason for the department and reopens it.
 */
function ReceptionReviewRow({
  wsId,
  item,
  onAccepted,
}: {
  wsId: string;
  item: DispatchListItem;
  onAccepted?: () => void;
}) {
  const { t } = useT("office");
  const qc = useQueryClient();
  const updateIssue = useUpdateIssue();
  const createComment = useCreateComment(item.issue.id);

  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<unknown>(null);

  const label = item.unit_path || t(($) => $.page.reception.unknown_department);
  const mention = subjectMention(
    item.issue.assignee_type,
    item.issue.assignee_id,
    label,
  );
  const pending = updateIssue.isPending || createComment.isPending;
  const trimmed = reason.trim();

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: workbenchKeys.all(wsId) });
  };

  const accept = async () => {
    setError(null);
    try {
      await updateIssue.mutateAsync({ id: item.issue.id, status: "done" });
      refresh();
      onAccepted?.();
    } catch (err) {
      setError(err);
    }
  };

  const reject = async () => {
    if (!trimmed) return;
    setError(null);
    try {
      await createComment.mutateAsync({
        content: rejectionComment(mention, trimmed),
      });
      await updateIssue.mutateAsync({ id: item.issue.id, status: "in_progress" });
      setReason("");
      setOpen(false);
      refresh();
    } catch (err) {
      setError(err);
    }
  };

  return (
    <li className="flex flex-col gap-1.5 rounded-lg border border-border/50 p-2">
      <div className="flex flex-col">
        <span className="text-micro text-muted-foreground">{item.issue.identifier}</span>
        <span className="line-clamp-2 font-medium">{item.issue.title}</span>
        <span className="truncate text-micro text-muted-foreground">{label}</span>
      </div>
      {open ? (
        <div className="flex flex-col gap-1.5">
          <Textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder={t(($) => $.page.reception.reason_placeholder)}
            aria-label={t(($) => $.page.reception.reason_label)}
            rows={2}
            maxLength={4000}
            className="min-h-12 resize-none text-caption"
            autoFocus
          />
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              type="button"
              size="sm"
              onClick={() => void reject()}
              disabled={pending || !trimmed}
            >
              {pending ? <Spinner /> : <RotateCcw />}
              {t(($) => $.page.reception.send_back)}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setOpen(false);
                setError(null);
              }}
              disabled={pending}
            >
              {t(($) => $.page.reception.cancel)}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-1.5">
          <Button
            type="button"
            size="sm"
            onClick={() => void accept()}
            disabled={pending}
          >
            {updateIssue.isPending ? <Spinner /> : <Check />}
            {t(($) => $.page.reception.accept)}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setOpen(true)}
            disabled={pending}
          >
            <RotateCcw />
            {t(($) => $.page.reception.reject)}
          </Button>
        </div>
      )}
      {error ? (
        <p role="alert" className="text-destructive">
          {open
            ? t(($) => $.page.reception.reject_failed)
            : t(($) => $.page.reception.accept_failed)}
        </p>
      ) : null}
    </li>
  );
}
