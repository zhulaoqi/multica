"use client";

import type { Attachment } from "@multica/core/types";
import { Paperclip, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  useAttachmentPreview,
  useResignedInlineMedia,
} from "../editor";

function DispatchImageThumbnail({
  attachment,
  onOpen,
}: {
  attachment: Attachment;
  onOpen: () => void;
}) {
  const { t } = useT("workbench");
  const pickedUrl =
    attachment.download_url || attachment.markdown_url || attachment.url;
  const { url } = useResignedInlineMedia(attachment.id, pickedUrl, true);

  return (
    <button
      type="button"
      aria-label={t(($) => $.page.preview_attachment, {
        name: attachment.filename,
      })}
      onClick={onOpen}
      className="group/image relative size-16 overflow-hidden rounded-lg border bg-muted outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <img
        src={url}
        alt={attachment.filename}
        className="size-full object-cover transition-transform group-hover/image:scale-105"
      />
    </button>
  );
}

/**
 * Request attachments shared by the draft and the sent user bubble. Images
 * stay compact and previewable; other documents keep a filename chip.
 */
export function DispatchAttachmentList({
  attachments,
  onRemove,
  className,
}: {
  attachments: readonly Attachment[];
  onRemove?: (id: string) => void;
  className?: string;
}) {
  const { t } = useT("workbench");
  const preview = useAttachmentPreview();

  if (attachments.length === 0) return null;

  return (
    <>
      <ul className={cn("flex flex-wrap gap-2", className)}>
        {attachments.map((attachment) => {
          const image = attachment.content_type.startsWith("image/");
          return (
            <li key={attachment.id} className="relative">
              {image ? (
                <DispatchImageThumbnail
                  attachment={attachment}
                  onOpen={() =>
                    preview.tryOpen({ kind: "full", attachment })
                  }
                />
              ) : (
                <div className="flex max-w-60 items-center gap-1.5 rounded-md border bg-muted/40 py-1 pr-2 pl-2 text-caption">
                  <Paperclip
                    className="size-3 shrink-0 text-muted-foreground"
                    aria-hidden
                  />
                  <span className="min-w-0 flex-1 truncate">
                    {attachment.filename}
                  </span>
                </div>
              )}
              {onRemove ? (
                <button
                  type="button"
                  aria-label={t(($) => $.page.remove_attachment, {
                    name: attachment.filename,
                  })}
                  onClick={() => onRemove(attachment.id)}
                  className={cn(
                    "rounded-full text-muted-foreground hover:bg-muted hover:text-foreground",
                    image
                      ? "absolute -top-1.5 -right-1.5 flex size-5 items-center justify-center border bg-background shadow-xs"
                      : "ml-1 inline-flex size-5 items-center justify-center",
                  )}
                >
                  <X className="size-3" />
                </button>
              ) : null}
            </li>
          );
        })}
      </ul>
      {preview.modal}
    </>
  );
}
