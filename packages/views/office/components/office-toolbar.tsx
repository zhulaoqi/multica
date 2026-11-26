"use client";

import { BellOff, MapPin, Star, Waves } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { SeasonTheme } from "../game/season";

export interface OfficeToolbarProps {
  season: SeasonTheme;
  soundOn: boolean;
  minimapOn: boolean;
  onToggleSound: () => void;
  onToggleMinimap: () => void;
  onOpenTrophies: () => void;
  /** Company-wide trophies are hidden on the C-end, which shows one person's work. */
  showTrophies?: boolean;
  className?: string;
}

export function OfficeToolbar({
  season,
  soundOn,
  minimapOn,
  onToggleSound,
  onToggleMinimap,
  onOpenTrophies,
  showTrophies = true,
  className,
}: OfficeToolbarProps) {
  const { t } = useT("office");
  return (
    <div
      className={cn(
        "flex items-center gap-0.5 rounded-lg bg-surface-raised/95 p-1 shadow-sm ring-1 ring-surface-border",
        className,
      )}
    >
      <span
        className="mr-1 flex items-center gap-1.5 rounded-md px-2 py-1 text-caption text-muted-foreground"
        title={t(($) => $.page.toolbar.season, {
          season: t(($) => $.page.season[season.id]),
        })}
      >
        <span
          aria-hidden
          className="size-2 rounded-full"
          style={{ backgroundColor: season.accent }}
        />
        {t(($) => $.page.season[season.id])}
      </span>
      {showTrophies ? (
        <IconButton
          label={t(($) => $.page.toolbar.trophies)}
          className="text-muted-foreground"
          onClick={onOpenTrophies}
          data-testid="office-toolbar-trophies"
        >
          <Star />
        </IconButton>
      ) : null}
      <IconButton
        label={
          minimapOn
            ? t(($) => $.page.toolbar.minimap_hide)
            : t(($) => $.page.toolbar.minimap_show)
        }
        className="text-muted-foreground"
        onClick={onToggleMinimap}
        aria-pressed={minimapOn}
      >
        <MapPin />
      </IconButton>
      <IconButton
        label={
          soundOn
            ? t(($) => $.page.toolbar.sound_off)
            : t(($) => $.page.toolbar.sound_on)
        }
        className="text-muted-foreground"
        onClick={onToggleSound}
        aria-pressed={soundOn}
        data-testid="office-toolbar-sound"
      >
        {soundOn ? <Waves /> : <BellOff />}
      </IconButton>
    </div>
  );
}
