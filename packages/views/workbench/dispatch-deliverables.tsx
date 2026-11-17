"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { FileText } from "@multica/ui/icons";
import {
  collectDeliverableFiles,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
import { childIssuesOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import type { TimelineEntry } from "@multica/core/types";
import { useT } from "../i18n";
import { formatBytes } from "../common/format-bytes";

/**
 * Every file the department produced for a dispatch, gathered from the root
 * issue and every sub-issue and folded into deliverables (one row per file,
 * its newest upload). The requester should not have to open five issues to
 * find the deck.
 *
 * The same rules as the issue's own deliverables apply: only files posted in
 * comments count, and a re-upload under the same name is a new version.
 */
export function DispatchDeliverables({ wsId, issueId }: { wsId: string; issueId: string }) {
  const { t } = useT("workbench");
  const { data: children = [] } = useQuery(childIssuesOptions(wsId, issueId));
  const issueIds = [issueId, ...children.map((child) => child.id)];

  const timelines = useQueries({
    queries: issueIds.map((id) => issueTimelineOptions(id)),
    combine: (results) =>
      results.flatMap((result) => (result.data ?? []) as TimelineEntry[]),
  });

  const files = collectDeliverableFiles(
    timelines.filter((entry) => entry.type === "comment"),
  );
  if (files.length === 0) return null;

  return (
    <section
      aria-label={t(($) => $.deliverables.title)}
      className="flex flex-col gap-1.5"
    >
      <p className="text-caption font-medium text-muted-foreground">
        {t(($) => $.deliverables.title)}
        <span className="ml-1.5 font-normal">{files.length}</span>
      </p>
      <ul className="flex flex-col gap-1">
        {files.map((file) => (
          <li key={file.key}>
            <DeliverableRow file={file} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function DeliverableRow({ file }: { file: DeliverableFile }) {
  const { t } = useT("workbench");
  const attachment = file.latest;
  const href = attachment.attachment_download_url ?? attachment.download_url ?? attachment.url;
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-caption text-foreground transition-colors hover:bg-muted"
    >
      <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="min-w-0 flex-1 truncate">{attachment.filename}</span>
      {file.versions.length > 1 ? (
        <span className="shrink-0 text-muted-foreground">
          {t(($) => $.deliverables.version_count, {
            count: file.versions.length,
          })}
        </span>
      ) : null}
      <span className="shrink-0 text-muted-foreground">
        {formatBytes(attachment.size_bytes)}
      </span>
    </a>
  );
}
