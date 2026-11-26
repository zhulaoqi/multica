"use client";

import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export interface OfficeBriefProps {
  completed: number;
  running: number;
  /** Today's top finisher, or null when nobody finished anything yet. */
  busiest: string | null;
  className?: string;
}

/**
 * A one-glance summary for the C-end office. The floor runs itself, so
 * someone coming back after a while needs to know what happened without
 * reading individual agents.
 */
export function OfficeBrief({
  completed,
  running,
  busiest,
  className,
}: OfficeBriefProps) {
  const { t } = useT("office");
  if (completed === 0 && running === 0 && !busiest) return null;

  const items = [
    t(($) => $.page.brief.completed, { count: completed }),
    t(($) => $.page.brief.running, { count: running }),
  ];
  if (busiest) items.push(t(($) => $.page.brief.busiest, { name: busiest }));

  return (
    <div
      data-testid="office-brief"
      className={cn(
        "pointer-events-none flex flex-wrap items-center gap-x-1.5 rounded-lg bg-surface-raised/95 px-3 py-1.5 text-caption text-muted-foreground shadow-sm ring-1 ring-surface-border",
        className,
      )}
    >
      {items.map((item, index) => (
        <span key={index} className="flex items-center gap-x-1.5">
          {index > 0 ? <span aria-hidden>·</span> : null}
          <span>{item}</span>
        </span>
      ))}
    </div>
  );
}
