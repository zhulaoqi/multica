"use client";

import { Lock, Star, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { DepartmentRoom } from "../game/departments";
import type { SeasonTheme } from "../game/season";
import { trophyBoard, type TrophyStats } from "../game/trophies";

export interface OfficeTrophyPanelProps {
  stats: TrophyStats;
  departments: DepartmentRoom[];
  season: SeasonTheme;
  onClose: () => void;
  className?: string;
}

export function OfficeTrophyPanel({
  stats,
  departments,
  season,
  onClose,
  className,
}: OfficeTrophyPanelProps) {
  const { t } = useT("office");
  const trophies = trophyBoard(stats);
  const earned = trophies.filter((trophy) => trophy.earned).length;

  return (
    <section
      aria-label={t(($) => $.page.trophies.title)}
      className={cn(
        "flex max-h-[min(32rem,80%)] w-80 flex-col overflow-hidden rounded-xl bg-surface-raised/95 shadow-xl ring-1 ring-surface-border",
        className,
      )}
    >
      <header className="flex items-center justify-between border-b px-3 py-2">
        <div className="flex items-center gap-2">
          <Star className="size-4 text-warning" aria-hidden />
          <h2 className="text-body font-medium">{t(($) => $.page.trophies.title)}</h2>
          <span className="text-caption text-muted-foreground tabular-nums">
            {earned} / {trophies.length}
          </span>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t(($) => $.page.trophies.close)}
          className="text-muted-foreground hover:text-foreground"
        >
          <X className="size-4" aria-hidden />
        </button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        <div className="mb-3 flex items-center justify-between rounded-lg border border-border/60 bg-muted/30 px-3 py-2">
          <div>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.page.trophies.season)}
            </p>
            <p className="flex items-center gap-1.5 text-body">
              <span
                aria-hidden
                className="size-2 rounded-full"
                style={{ backgroundColor: season.accent }}
              />
              {t(($) => $.page.season[season.id])}
            </p>
          </div>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.page.trophies.archived, { count: stats.archived })}
          </p>
        </div>

        <ul className="mb-3 grid grid-cols-2 gap-1.5">
          {trophies.map((trophy) => (
            <li
              key={trophy.id}
              className={cn(
                "flex items-start gap-1.5 rounded-lg border p-2",
                trophy.earned
                  ? "border-warning/40 bg-warning/10"
                  : "border-border/60 bg-muted/20 text-muted-foreground",
              )}
            >
              {trophy.earned ? (
                <Star className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden />
              ) : (
                <Lock className="mt-0.5 size-3.5 shrink-0" aria-hidden />
              )}
              <div className="min-w-0">
                <p className="text-caption font-medium">
                  {t(($) => $.page.trophies.items[trophy.id].name)}
                </p>
                <p className="text-[10px] leading-tight text-muted-foreground">
                  {t(($) => $.page.trophies.items[trophy.id].hint)}
                </p>
              </div>
            </li>
          ))}
        </ul>

        <h3 className="mb-1.5 text-caption font-medium text-muted-foreground">
          {t(($) => $.page.trophies.departments)}
        </h3>
        {departments.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.page.trophies.departments_empty)}
          </p>
        ) : (
          <ul className="flex flex-col gap-1">
            {departments.map((department) => (
              <li
                key={department.unitId}
                className="flex items-center justify-between rounded-md px-1 py-0.5"
              >
                <span className="min-w-0 flex-1 truncate text-caption">
                  {department.name}
                </span>
                <span
                  className="ml-2 flex items-center gap-0.5"
                  aria-label={t(($) => $.page.roster.level, {
                    level: department.level,
                  })}
                >
                  {Array.from({ length: 5 }, (_, index) => (
                    <Star
                      key={index}
                      aria-hidden
                      className={cn(
                        "size-2.5",
                        index < department.stars
                          ? "text-warning"
                          : "text-faint-foreground",
                      )}
                    />
                  ))}
                  <span className="ml-1 text-[10px] text-muted-foreground tabular-nums">
                    {t(($) => $.page.roster.level, { level: department.level })}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
