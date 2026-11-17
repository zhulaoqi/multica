"use client";

import { MessageSquareText, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export interface MeetingNote {
  id: string;
  title: string;
  /** Current dispatch phase, shown as the note's status. */
  phase: string;
}

export interface OfficeMeetingPanelProps {
  notes: MeetingNote[];
  onClose: () => void;
  className?: string;
}

const NOTE_TONES = [
  "bg-warning/15 border-warning/40",
  "bg-info/15 border-info/40",
  "bg-success/15 border-success/40",
  "bg-destructive/10 border-destructive/30",
] as const;

/**
 * The meeting-room board carries the office's open work, one sticky per
 * dispatch. Nothing is written here: a note disappears when its dispatch
 * finishes, so the board cannot disagree with the workbench.
 */
export function OfficeMeetingPanel({
  notes,
  onClose,
  className,
}: OfficeMeetingPanelProps) {
  const { t } = useT("office");
  const phaseLabel = (phase: string): string => {
    switch (phase) {
      case "decomposing":
        return t(($) => $.page.meeting.phases.decomposing);
      case "progress":
        return t(($) => $.page.meeting.phases.progress);
      case "in_review":
        return t(($) => $.page.meeting.phases.in_review);
      default:
        return t(($) => $.page.meeting.phases.queued);
    }
  };
  return (
    <section
      aria-label={t(($) => $.page.meeting.title)}
      className={cn(
        "w-80 rounded-xl bg-surface-raised/95 p-3 shadow-xl ring-1 ring-surface-border",
        className,
      )}
    >
      <header className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <MessageSquareText className="size-4 text-info" aria-hidden />
          <h2 className="text-body font-medium">{t(($) => $.page.meeting.title)}</h2>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t(($) => $.page.meeting.close)}
          className="text-muted-foreground hover:text-foreground"
        >
          <X className="size-4" aria-hidden />
        </button>
      </header>
      {notes.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.page.meeting.empty)}</p>
      ) : (
        <ul className="flex max-h-64 flex-col gap-1.5 overflow-y-auto">
          {notes.map((note, index) => (
            <li
              key={note.id}
              className={cn(
                "rounded-md border px-2 py-1.5",
                NOTE_TONES[index % NOTE_TONES.length],
              )}
            >
              <p className="truncate text-caption font-medium">{note.title}</p>
              <p className="text-[10px] text-muted-foreground">
                {phaseLabel(note.phase)}
              </p>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
