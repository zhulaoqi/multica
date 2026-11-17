"use client";

import {
  BellOff,
  Camera,
  MapPin,
  SmilePlus,
  Star,
  Waves,
} from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { PLAYER_EMOTES, type PlayerEmoteId } from "../game/emotes";
import type { SeasonTheme } from "../game/season";

export interface OfficeToolbarProps {
  season: SeasonTheme;
  soundOn: boolean;
  minimapOn: boolean;
  photoMode: boolean;
  emotesOpen: boolean;
  onToggleSound: () => void;
  onToggleMinimap: () => void;
  onTogglePhoto: () => void;
  onToggleEmotes: () => void;
  onOpenTrophies: () => void;
  className?: string;
}

export function OfficeToolbar({
  season,
  soundOn,
  minimapOn,
  photoMode,
  emotesOpen,
  onToggleSound,
  onToggleMinimap,
  onTogglePhoto,
  onToggleEmotes,
  onOpenTrophies,
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
      <IconButton
        label={t(($) => $.page.toolbar.emotes)}
        className="text-muted-foreground"
        onClick={onToggleEmotes}
        aria-expanded={emotesOpen}
        data-testid="office-toolbar-emotes"
      >
        <SmilePlus />
      </IconButton>
      <IconButton
        label={t(($) => $.page.toolbar.trophies)}
        className="text-muted-foreground"
        onClick={onOpenTrophies}
        data-testid="office-toolbar-trophies"
      >
        <Star />
      </IconButton>
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
        label={t(($) => $.page.toolbar.photo)}
        className="text-muted-foreground"
        onClick={onTogglePhoto}
        aria-pressed={photoMode}
      >
        <Camera />
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

export interface OfficeEmoteWheelProps {
  onEmote: (kind: PlayerEmoteId) => void;
  onClose: () => void;
  className?: string;
}

export function OfficeEmoteWheel({
  onEmote,
  onClose,
  className,
}: OfficeEmoteWheelProps) {
  const { t } = useT("office");
  return (
    <div
      role="group"
      aria-label={t(($) => $.page.emotes.title)}
      className={cn(
        "w-56 rounded-lg bg-surface-raised/95 p-2 shadow-lg ring-1 ring-surface-border",
        className,
      )}
    >
      <div className="mb-1.5 flex items-center justify-between px-1">
        <span className="text-caption font-medium">{t(($) => $.page.emotes.title)}</span>
        <button
          type="button"
          onClick={onClose}
          className="text-caption text-muted-foreground hover:text-foreground"
        >
          {t(($) => $.page.interact.close)}
        </button>
      </div>
      <div className="grid grid-cols-2 gap-1">
        {PLAYER_EMOTES.map((emote) => (
          <button
            key={emote.id}
            type="button"
            onClick={() => {
              onEmote(emote.id);
              onClose();
            }}
            className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption hover:bg-sidebar-accent"
          >
            <kbd className="rounded border border-border bg-muted px-1 font-mono text-[10px] text-muted-foreground">
              {emote.key}
            </kbd>
            {t(($) => $.page.emotes[emote.id])}
          </button>
        ))}
      </div>
    </div>
  );
}

export interface OfficePhotoBarProps {
  onSave: () => void;
  onExit: () => void;
  saved: boolean;
}

export function OfficePhotoBar({ onSave, onExit, saved }: OfficePhotoBarProps) {
  const { t } = useT("office");
  return (
    <div className="flex items-center gap-2 rounded-full bg-surface-raised/95 px-3 py-1.5 shadow-lg ring-1 ring-surface-border">
      <span className="text-caption text-muted-foreground">
        {saved ? t(($) => $.page.photo.saved) : t(($) => $.page.photo.hint)}
      </span>
      <button
        type="button"
        onClick={onSave}
        className="rounded-full bg-foreground px-3 py-1 text-caption font-medium text-background"
      >
        {t(($) => $.page.photo.save)}
      </button>
      <button
        type="button"
        onClick={onExit}
        className="rounded-full border border-border px-3 py-1 text-caption"
      >
        {t(($) => $.page.photo.exit)}
      </button>
    </div>
  );
}
