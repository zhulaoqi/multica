"use client";

import { useRef } from "react";
import { ArrowUp, Building2, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

const MAX_DISPATCH_TEXT = 4000;

/**
 * The workbench input. Enter sends; Shift+Enter keeps a newline. IME
 * composition (Chinese/Japanese/Korean candidate selection) must never send
 * on the Enter that commits a candidate, so the composer tracks composition
 * both via the DOM flag and a ref for browsers that lag on `isComposing`.
 */
export function DispatchComposer({
  value,
  onChange,
  onSubmit,
  pending,
  unitPath,
  onClearUnit,
  focusToken = 0,
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (text: string) => void;
  pending: boolean;
  unitPath?: string | null;
  onClearUnit?: () => void;
  focusToken?: number;
}) {
  const { t } = useT("workbench");
  const composingRef = useRef(false);

  const submit = () => {
    const text = value.trim();
    if (!text || pending) return;
    onSubmit(text);
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
      className="flex flex-col gap-2 rounded-xl border bg-background p-2 shadow-xs focus-within:border-ring/60"
    >
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
      <Textarea
        // Remounting on a focus request re-runs autoFocus; the value stays
        // controlled, so the draft is not lost.
        key={focusToken}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={t(($) => $.page.placeholder)}
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
        <span className="hidden truncate text-caption text-muted-foreground sm:block">
          {t(($) => $.page.composer_hint)}
        </span>
        <Button
          type="submit"
          size="icon-sm"
          aria-label={t(($) => $.page.send)}
          disabled={pending || !value.trim()}
          className={cn("ml-auto")}
        >
          {pending ? <Spinner className="size-3.5" /> : <ArrowUp />}
        </Button>
      </div>
    </form>
  );
}
