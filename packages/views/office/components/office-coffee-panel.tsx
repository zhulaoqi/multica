"use client";

import { MessageCircleQuestion, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { CoffeePromptId } from "../game/coffee";

export interface OfficeCoffeePanelProps {
  prompt: CoffeePromptId;
  stuckNames: string[];
  onAnother: () => void;
  onClose: () => void;
  className?: string;
}

/**
 * The kitchen watercooler: a rotating prompt for a short conversation, plus
 * the agents currently blocked so the break has somewhere concrete to go.
 */
export function OfficeCoffeePanel({
  prompt,
  stuckNames,
  onAnother,
  onClose,
  className,
}: OfficeCoffeePanelProps) {
  const { t } = useT("office");
  return (
    <section
      aria-label={t(($) => $.page.coffee.title)}
      className={cn(
        "w-80 rounded-xl bg-surface-raised/95 p-3 shadow-xl ring-1 ring-surface-border",
        className,
      )}
    >
      <header className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <MessageCircleQuestion className="size-4 text-warning" aria-hidden />
          <h2 className="text-body font-medium">{t(($) => $.page.coffee.title)}</h2>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t(($) => $.page.coffee.close)}
          className="text-muted-foreground hover:text-foreground"
        >
          <X className="size-4" aria-hidden />
        </button>
      </header>
      <p className="mb-3 rounded-lg border border-border/60 bg-muted/30 px-3 py-2 text-body">
        {t(($) => $.page.coffee.prompts[prompt])}
      </p>
      {stuckNames.length > 0 ? (
        <p className="mb-3 text-caption text-muted-foreground">
          {t(($) => $.page.coffee.stuck, { names: stuckNames.join(", ") })}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button size="sm" variant="outline" onClick={onAnother}>
          {t(($) => $.page.coffee.another)}
        </Button>
      </div>
    </section>
  );
}
