"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, RotateCcw, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useCreateComment, useUpdateIssue } from "@multica/core/issues/mutations";
import type { DispatchListItem } from "@multica/core/workbench";
import { workbenchKeys } from "@multica/core/workbench";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT } from "../i18n";
import { rejectionComment, subjectMention } from "./dispatch-actions";

/**
 * The requester's verdict on a delivered result.
 *
 * Accepting closes the root issue, which is what the completion listener
 * treats as delivery. Rejecting posts the reason as a comment that mentions
 * the department's owner (so the next run has it) and returns the issue to
 * in-progress, where the leader can pick it up again.
 *
 * Both writes wait for the server; a failed write stays on screen with its
 * error and keeps the typed reason, so nothing is retyped or lost.
 */
export function DispatchReview({ item }: { item: DispatchListItem }) {
  const { t } = useT("workbench");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const updateIssue = useUpdateIssue();
  const createComment = useCreateComment(item.issue.id);
  const { getActorName } = useActorName();

  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<unknown>(null);

  const assigneeName =
    item.issue.assignee_type && item.issue.assignee_id
      ? getActorName(item.issue.assignee_type, item.issue.assignee_id)
      : (item.unit_path || t(($) => $.page.unknown_department));
  const mention = subjectMention(
    item.issue.assignee_type,
    item.issue.assignee_id,
    assigneeName,
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
    <div className="flex flex-col gap-2">
      {open ? (
        <div className="flex flex-col gap-1.5">
          <Textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder={t(($) => $.review.reason_placeholder)}
            aria-label={t(($) => $.review.reason_label)}
            rows={2}
            maxLength={4000}
            className="min-h-12 resize-none text-caption"
          />
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="default"
              size="sm"
              onClick={() => void reject()}
              disabled={pending || !trimmed}
            >
              {createComment.isPending || updateIssue.isPending ? <Spinner /> : <RotateCcw />}
              {t(($) => $.review.submit_reject)}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setOpen(false);
                setError(null);
              }}
              disabled={pending}
            >
              <X />
              {t(($) => $.review.cancel)}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="default"
            size="sm"
            onClick={() => void accept()}
            disabled={pending}
          >
            {updateIssue.isPending ? <Spinner /> : <Check />}
            {t(($) => $.review.accept)}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setOpen(true)} disabled={pending}>
            <RotateCcw />
            {t(($) => $.review.reject)}
          </Button>
        </div>
      )}
      {error ? (
        <p role="alert" className="text-caption text-destructive">
          {open ? t(($) => $.review.reject_failed) : t(($) => $.review.accept_failed)}
        </p>
      ) : null}
    </div>
  );
}
