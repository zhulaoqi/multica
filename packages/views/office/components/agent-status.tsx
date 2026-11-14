"use client";

import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { AgentDeskStatus } from "../scene/characters";

const STATUS_DOT: Record<AgentDeskStatus, string> = {
  working: "bg-brand",
  queued: "bg-warning",
  idle: "bg-muted-foreground/40",
  offline: "bg-muted-foreground/40",
};

export function AgentStatusBadge({
  status,
  className,
}: {
  status: AgentDeskStatus;
  className?: string;
}) {
  const { t } = useT("office");

  return (
    <span
      className={cn(
        "flex items-center gap-1.5 text-caption text-muted-foreground",
        className,
      )}
    >
      <span
        aria-hidden="true"
        className={cn("size-1.5 rounded-full", STATUS_DOT[status])}
      />
      {t(($) => $.page.status[status])}
    </span>
  );
}

/** Single-letter stand-in for avatars that have no uploaded image. */
export function initials(name: string) {
  return name.trim().charAt(0).toUpperCase() || "?";
}
