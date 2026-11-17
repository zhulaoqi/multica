"use client";

import { useEffect } from "react";
import { Sparkles } from "@multica/ui/icons";
import { useT } from "../../i18n";

export type OfficeCelebrationEvent =
  | { kind: "company"; level: number }
  | { kind: "weekly"; count: number };

const DISMISS_MS = 6_000;

/**
 * A short banner for the two moments the office celebrates together: a new
 * company level, and the shared weekly goal landing. It dismisses itself so
 * it never competes with the room.
 */
export function OfficeCelebration({
  event,
  onDismiss,
}: {
  event: OfficeCelebrationEvent;
  onDismiss: () => void;
}) {
  const { t } = useT("office");

  useEffect(() => {
    const timer = window.setTimeout(onDismiss, DISMISS_MS);
    return () => window.clearTimeout(timer);
  }, [event, onDismiss]);

  return (
    <div
      role="status"
      className="pointer-events-none absolute inset-x-0 top-3 z-10 flex justify-center"
    >
      <div className="flex animate-in items-center gap-2 rounded-full border border-warning/40 bg-surface-raised px-4 py-2 shadow-lg fade-in slide-in-from-top-2">
        <Sparkles className="size-4 shrink-0 text-warning" />
        <div className="min-w-0 text-left">
          <p className="text-body font-medium">
            {event.kind === "company"
              ? t(($) => $.page.celebration.company_level, { level: event.level })
              : t(($) => $.page.celebration.weekly_goal)}
          </p>
          <p className="text-caption text-muted-foreground">
            {event.kind === "company"
              ? t(($) => $.page.celebration.company_level_hint)
              : t(($) => $.page.celebration.weekly_goal_hint, { count: event.count })}
          </p>
        </div>
      </div>
    </div>
  );
}
