"use client";

import { useQuery } from "@tanstack/react-query";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { ReadonlyContent } from "../editor/readonly-content";
import { useT } from "../i18n";
import { dispatchResult } from "./dispatch-result";

/**
 * What the department delivered, read from the root issue's newest agent
 * comment. Only fetched once the work is delivered, so in-flight dispatches
 * do not load a timeline nobody reads yet.
 */
export function DispatchResultBlock({ issueId }: { issueId: string }) {
  const { t } = useT("workbench");
  const { data: result, isPending } = useQuery({
    ...issueTimelineOptions(issueId),
    select: dispatchResult,
  });
  if (isPending) return null;
  if (!result) {
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
        {result.actor_name
          ? t(($) => $.result.by, { name: result.actor_name })
          : t(($) => $.result.title)}
      </p>
      <div className="max-h-80 overflow-y-auto">
        <ReadonlyContent content={result.content ?? ""} attachments={result.attachments} />
      </div>
    </section>
  );
}
