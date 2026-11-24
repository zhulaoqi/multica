"use client";

import { useQuery } from "@tanstack/react-query";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import type { Attachment } from "@multica/core/types";
import type { DispatchTrailResult } from "@multica/core/workbench";
import { ReadonlyContent } from "../editor/readonly-content";
import { useT } from "../i18n";
import { dispatchResult } from "./dispatch-result";

/**
 * What the department delivered, read from the root issue's newest agent
 * comment. Only fetched once the work is delivered, so in-flight dispatches
 * do not load a timeline nobody reads yet.
 *
 * A dispatch delivered into another workspace passes the server's trail
 * result instead: this workspace's timeline query cannot see that issue.
 */
export function DispatchResultBlock({
  issueId,
  result,
}: {
  issueId: string;
  /** Pre-loaded result from the cross-workspace trail, when there is one. */
  result?: DispatchTrailResult | null;
}) {
  const { t } = useT("workbench");
  const external = result !== undefined;
  const { data: fetched, isPending } = useQuery({
    ...issueTimelineOptions(issueId),
    select: dispatchResult,
    enabled: !external,
  });
  type ResultView = {
    actor_name?: string | null;
    content?: string | null;
    attachments?: Attachment[];
  };
  const shown: ResultView | null | undefined = external
    ? result
      ? {
          actor_name: result.actor_name,
          content: result.content,
          attachments: result.attachments as Attachment[],
        }
      : null
    : fetched;
  if (!external && isPending) return null;
  if (!shown) {
    return (
      <p className="text-caption text-muted-foreground">{t(($) => $.result.empty)}</p>
    );
  }
  return (
    <section
      aria-label={t(($) => $.result.title)}
      className="flex flex-col gap-1.5 rounded-lg bg-muted/50 px-3 py-2.5"
    >
      <p className="text-caption font-medium text-muted-foreground">
        {shown.actor_name
          ? t(($) => $.result.by, { name: shown.actor_name })
          : t(($) => $.result.title)}
      </p>
      <div className="max-h-80 overflow-y-auto">
        <ReadonlyContent
          content={shown.content ?? ""}
          attachments={(shown.attachments ?? []) as Attachment[]}
        />
      </div>
    </section>
  );
}
