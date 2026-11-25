"use client";

import { useRef, useState } from "react";
import type { Attachment } from "@multica/core/types";
import {
  ArrowUp,
  AtSign,
  Building2,
  FolderKanban,
  MessageSquareText,
  Paperclip,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { DispatchAttachmentList } from "./dispatch-attachment-list";

const MAX_DISPATCH_TEXT = 4000;

/** A file already uploaded, waiting for the message it belongs to. */
export type ComposerAttachment = Attachment;

/** Someone the requester can pull into the conversation by name. */
export interface ComposerMention {
  type: "agent" | "squad" | "member";
  id: string;
  label: string;
  /** Secondary line: the agent's role, the squad's size, and so on. */
  hint?: string;
}

function mentionToken(mention: ComposerMention): string {
  const safe = mention.label.replace(/[[\]\\()]/g, (char) => `\\${char}`);
  return `[@${safe}](mention://${mention.type}/${mention.id})`;
}

/**
 * The workbench input. Enter sends; Shift+Enter keeps a newline. IME
 * composition (Chinese/Japanese/Korean candidate selection) must never send
 * on the Enter that commits a candidate, so the composer tracks composition
 * both via the DOM flag and a ref for browsers that lag on `isComposing`.
 *
 * Files upload before the message is sent; until they finish the send button
 * stays disabled, so a request never reaches the department without the deck
 * or screenshot it referred to.
 */
export function DispatchComposer({
  value,
  onChange,
  onSubmit,
  pending,
  unitPath,
  onClearUnit,
  projectName,
  onClearProject,
  threadTitle,
  onExitThread,
  attachments = [],
  onAttachFiles,
  onRemoveAttachment,
  uploading = false,
  mentions = [],
  focusToken = 0,
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (text: string) => void;
  pending: boolean;
  unitPath?: string | null;
  onClearUnit?: () => void;
  /** The project the request will be handed through, when one is chosen. */
  projectName?: string | null;
  onClearProject?: () => void;
  /** Set while replying inside a dispatch thread instead of starting one. */
  threadTitle?: string | null;
  onExitThread?: () => void;
  attachments?: readonly ComposerAttachment[];
  onAttachFiles?: (files: FileList | readonly File[]) => void;
  onRemoveAttachment?: (id: string) => void;
  uploading?: boolean;
  mentions?: readonly ComposerMention[];
  focusToken?: number;
}) {
  const { t } = useT("workbench");
  const composingRef = useRef(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [draggingFiles, setDraggingFiles] = useState(false);

  const submit = () => {
    const text = value.trim();
    if (!text || pending || uploading) return;
    onSubmit(text);
  };

  const insertMention = (mention: ComposerMention) => {
    const el = inputRef.current;
    const start = el?.selectionStart ?? value.length;
    const end = el?.selectionEnd ?? value.length;
    const before = value.slice(0, start);
    const spacer = before.length > 0 && !/\s$/.test(before) ? " " : "";
    const token = `${spacer}${mentionToken(mention)} `;
    onChange(`${before}${token}${value.slice(end)}`);
    const caret = start + token.length;
    // The textarea is controlled; restore the caret after React applies the
    // new value so the requester can keep typing at the insertion point.
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(caret, caret);
    });
  };

  const inThread = threadTitle !== undefined && threadTitle !== null;

  return (
    <form
      data-testid="dispatch-composer"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
      onDragEnter={(event) => {
        if (!onAttachFiles || event.dataTransfer.types.includes("Files") === false) {
          return;
        }
        event.preventDefault();
        setDraggingFiles(true);
      }}
      onDragOver={(event) => {
        if (!onAttachFiles || event.dataTransfer.types.includes("Files") === false) {
          return;
        }
        event.preventDefault();
        event.dataTransfer.dropEffect = "copy";
      }}
      onDragLeave={(event) => {
        if (
          event.relatedTarget instanceof Node &&
          event.currentTarget.contains(event.relatedTarget)
        ) {
          return;
        }
        setDraggingFiles(false);
      }}
      onDrop={(event) => {
        setDraggingFiles(false);
        if (!onAttachFiles || event.dataTransfer.files.length === 0) return;
        event.preventDefault();
        onAttachFiles(event.dataTransfer.files);
      }}
      className={cn(
        "relative flex flex-col gap-2 rounded-xl border bg-background p-2 shadow-xs focus-within:border-ring/60",
        draggingFiles && "border-ring ring-2 ring-ring/20",
      )}
    >
      {draggingFiles ? (
        <div className="pointer-events-none absolute inset-1 z-10 flex items-center justify-center rounded-lg border border-dashed border-ring bg-background/90 text-body font-medium">
          {t(($) => $.page.drop_files)}
        </div>
      ) : null}
      {inThread ? (
        <div className="flex items-center gap-1.5 self-start rounded-md border bg-muted/50 py-0.5 pr-1 pl-2 text-caption">
          <MessageSquareText className="size-3.5 text-muted-foreground" aria-hidden />
          <span className="max-w-72 truncate">{threadTitle}</span>
          {onExitThread ? (
            <button
              type="button"
              aria-label={t(($) => $.page.leave_thread)}
              onClick={onExitThread}
              className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          ) : null}
        </div>
      ) : null}
      {unitPath !== undefined && unitPath !== null ? (
        <div className="flex items-center gap-1.5 self-start rounded-md border bg-muted/50 py-0.5 pr-1 pl-2 text-caption">
          <Building2 className="size-3.5 text-muted-foreground" aria-hidden />
          <span className="max-w-72 truncate">{unitPath || t(($) => $.page.unknown_department)}</span>
          {onClearUnit ? (
            <button
              type="button"
              aria-label={t(($) => $.page.route_automatically)}
              onClick={onClearUnit}
              className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          ) : null}
        </div>
      ) : null}
      {projectName !== undefined && projectName !== null ? (
        <div className="flex items-center gap-1.5 self-start rounded-md border bg-muted/50 py-0.5 pr-1 pl-2 text-caption">
          <FolderKanban className="size-3.5 text-muted-foreground" aria-hidden />
          <span className="max-w-72 truncate">{projectName}</span>
          {onClearProject ? (
            <button
              type="button"
              aria-label={t(($) => $.page.project_clear)}
              onClick={onClearProject}
              className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          ) : null}
        </div>
      ) : null}
      <DispatchAttachmentList
        attachments={attachments}
        onRemove={onRemoveAttachment}
      />
      <Textarea
        // Remounting on a focus request re-runs autoFocus; the value stays
        // controlled, so the draft is not lost.
        key={focusToken}
        ref={inputRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onPaste={(event) => {
          if (!onAttachFiles || event.clipboardData.files.length === 0) return;
          event.preventDefault();
          onAttachFiles(event.clipboardData.files);
        }}
        placeholder={
          inThread
            ? t(($) => $.page.followup_placeholder)
            : t(($) => $.page.placeholder)
        }
        maxLength={MAX_DISPATCH_TEXT}
        rows={2}
        autoFocus
        aria-label={t(($) => $.page.placeholder)}
        className="min-h-16 resize-none border-0 bg-transparent p-1.5 shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
        onCompositionStart={() => {
          composingRef.current = true;
        }}
        onCompositionEnd={() => {
          composingRef.current = false;
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.shiftKey) return;
          if (composingRef.current || event.nativeEvent.isComposing) return;
          event.preventDefault();
          submit();
        }}
      />
      <div className="flex items-center justify-between gap-2 px-1">
        <div className="flex min-w-0 items-center gap-0.5">
          {onAttachFiles ? (
            <>
              <input
                ref={fileRef}
                type="file"
                multiple
                className="sr-only"
                tabIndex={-1}
                aria-hidden
                onChange={(event) => {
                  const files = event.target.files;
                  if (files && files.length > 0) onAttachFiles(files);
                  event.target.value = "";
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.page.attach_files)}
                disabled={pending}
                onClick={() => fileRef.current?.click()}
              >
                <Paperclip />
              </Button>
            </>
          ) : null}
          {inThread && mentions.length > 0 ? (
            <Popover>
              <PopoverTrigger
                render={
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.page.mention)}
                    disabled={pending}
                  >
                    <AtSign />
                  </Button>
                }
              />
              <PopoverContent align="start" className="w-64 p-1">
                <p className="px-2 py-1 text-caption text-muted-foreground">
                  {t(($) => $.page.mention_label)}
                </p>
                <ul className="flex max-h-60 flex-col overflow-y-auto">
                  {mentions.map((mention) => (
                    <li key={`${mention.type}:${mention.id}`}>
                      <button
                        type="button"
                        className="flex w-full flex-col rounded-sm px-2 py-1 text-left text-body hover:bg-muted"
                        onClick={() => insertMention(mention)}
                      >
                        <span className="truncate">{mention.label}</span>
                        {mention.hint ? (
                          <span className="truncate text-caption text-muted-foreground">
                            {mention.hint}
                          </span>
                        ) : null}
                      </button>
                    </li>
                  ))}
                </ul>
              </PopoverContent>
            </Popover>
          ) : null}
          {uploading ? (
            <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
              <Spinner className="size-3" />
              {t(($) => $.page.uploading)}
            </span>
          ) : (
            <span className="hidden truncate text-caption text-muted-foreground sm:block">
              {inThread ? t(($) => $.page.thread_hint) : t(($) => $.page.composer_hint)}
            </span>
          )}
        </div>
        <Button
          type="submit"
          size="icon-sm"
          aria-label={t(($) => $.page.send)}
          disabled={pending || uploading || !value.trim()}
          className={cn("ml-auto")}
        >
          {pending ? <Spinner className="size-3.5" /> : <ArrowUp />}
        </Button>
      </div>
    </form>
  );
}
