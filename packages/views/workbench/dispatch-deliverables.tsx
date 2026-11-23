"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { FileText } from "@multica/ui/icons";
import {
  collectDeliverableFiles,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
import { childIssuesOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import type { TimelineEntry } from "@multica/core/types";
import type { DispatchDeliverable } from "@multica/core/workbench";
import { useT } from "../i18n";
import { formatBytes } from "../common/format-bytes";

/**
 * Every file the department produced for a dispatch, gathered from the root
 * issue and every sub-issue and folded into deliverables (one row per file,
 * its newest upload). The requester should not have to open five issues to
 * find the deck.
 *
 * The same rules as the issue's own deliverables apply: only files posted in
 * comments count, and a re-upload under the same name is a new version. A
 * dispatch delivered into another workspace receives the server's folded list
 * (`deliverables`) instead of querying issues this workspace cannot read.
 */
export function DispatchDeliverables({
  wsId,
  issueId,
  deliverables,
}: {
  wsId: string;
  issueId: string;
  /** Pre-folded files from the cross-workspace trail, when there are any. */
  deliverables?: DispatchDeliverable[];
}) {
  const { t } = useT("workbench");
  const external = deliverables !== undefined;
  const { data: children = [] } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !external,
  });
  const issueIds = external ? [] : [issueId, ...children.map((child) => child.id)];

  const timelines = useQueries({
    queries: issueIds.map((id) => issueTimelineOptions(id)),
    combine: (results) =>
      results.flatMap((result) => (result.data ?? []) as TimelineEntry[]),
  });

  const files: DeliverableRow[] = external
    ? (deliverables ?? []).map((file) => ({
        key: file.key,
        filename: file.filename,
        href: file.download_url,
        sizeBytes: file.size_bytes,
        versionCount: file.version_count,
      }))
    : collectDeliverableFiles(
        timelines.filter((entry) => entry.type === "comment"),
      ).map(fileRow);
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
            <DeliverableRowView file={file} />
          </li>
        ))}
      </ul>
    </section>
  );
}

interface DeliverableRow {
  key: string;
  filename: string;
  href: string;
  sizeBytes: number;
  versionCount: number;
}

function fileRow(file: DeliverableFile): DeliverableRow {
  const attachment = file.latest;
  return {
    key: file.key,
    filename: attachment.filename,
    href:
      attachment.attachment_download_url ??
      attachment.download_url ??
      attachment.url,
    sizeBytes: attachment.size_bytes,
    versionCount: file.versions.length,
  };
}

function DeliverableRowView({ file }: { file: DeliverableRow }) {
  const { t } = useT("workbench");
  return (
    <a
      href={file.href}
      target="_blank"
      rel="noreferrer"
      className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-caption text-foreground transition-colors hover:bg-muted"
    >
      <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="min-w-0 flex-1 truncate">{file.filename}</span>
      {file.versionCount > 1 ? (
        <span className="shrink-0 text-muted-foreground">
          {t(($) => $.deliverables.version_count, {
            count: file.versionCount,
          })}
        </span>
      ) : null}
      <span className="shrink-0 text-muted-foreground">
        {formatBytes(file.sizeBytes)}
      </span>
    </a>
  );
}
