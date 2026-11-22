"use client";

import { useState } from "react";
import type { OrgIntakeSource } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Plus, X } from "@multica/ui/icons";
import { useT } from "../i18n";

export interface IntakeSourceUnitChoice {
  id: string;
  path: string;
}

export interface IntakeSourceMemberChoice {
  user_id: string;
  label: string;
}

/**
 * Who may hand a department or project work: people and whole departments.
 * The editor owns only the chips and the picker; saving belongs to the caller
 * because the two surfaces persist through different endpoints.
 */
export function IntakeSourceEditor({
  sources,
  unitChoices,
  members,
  emptyLabel,
  disabled = false,
  onSave,
}: {
  sources: OrgIntakeSource[];
  unitChoices: IntakeSourceUnitChoice[];
  members: IntakeSourceMemberChoice[];
  /** Shown in place of chips when nothing is listed. */
  emptyLabel: string;
  disabled?: boolean;
  onSave: (next: Array<{ type: "user" | "org_unit"; id: string }>) => void;
}) {
  const { t } = useT("org");
  const [open, setOpen] = useState(false);
  const refs = sources.map((source) => ({ type: source.type, id: source.id }));
  const remove = (type: "user" | "org_unit", id: string) =>
    onSave(refs.filter((source) => !(source.type === type && source.id === id)));
  const add = (type: "user" | "org_unit", id: string) => {
    if (refs.some((source) => source.type === type && source.id === id)) return;
    onSave([...refs, { type, id }]);
    setOpen(false);
  };
  const unitLabel = (id: string) => unitChoices.find((choice) => choice.id === id)?.path ?? id.slice(0, 8);
  const memberLabel = (id: string) => members.find((member) => member.user_id === id)?.label ?? id.slice(0, 8);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {sources.length === 0 ? (
        <span className="text-caption text-muted-foreground">{emptyLabel}</span>
      ) : (
        sources.map((source) => {
          const label = source.type === "user" ? memberLabel(source.id) : unitLabel(source.id);
          return (
            <span
              key={`${source.type}:${source.id}`}
              className="inline-flex items-center gap-1 rounded-full border border-border/60 py-0.5 pr-1 pl-2 text-caption"
            >
              <span className="max-w-40 truncate">{label}</span>
              {disabled ? null : (
                <button
                  type="button"
                  aria-label={t(($) => $.intake_source_remove, { name: label })}
                  className="rounded-full p-0.5 text-muted-foreground hover:bg-sidebar-accent hover:text-foreground"
                  onClick={() => remove(source.type, source.id)}
                >
                  <X className="size-3" aria-hidden />
                </button>
              )}
            </span>
          );
        })
      )}
      {disabled ? null : (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <Button
                size="sm"
                variant="ghost"
                className="h-6 gap-1 px-1.5 text-caption text-muted-foreground"
              />
            }
          >
            <Plus className="size-3" aria-hidden />
            {t(($) => $.intake_source_add)}
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 p-0">
            <Command>
              <CommandInput placeholder={t(($) => $.picker_search)} />
              <CommandList>
                <CommandEmpty>{t(($) => $.picker_empty)}</CommandEmpty>
                {unitChoices.map((choice) => (
                  <CommandItem
                    key={`unit:${choice.id}`}
                    value={choice.path}
                    onSelect={() => add("org_unit", choice.id)}
                  >
                    <span className="min-w-0 flex-1 truncate">{choice.path}</span>
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.intake_source_department)}
                    </span>
                  </CommandItem>
                ))}
                {members.map((member) => (
                  <CommandItem
                    key={`member:${member.user_id}`}
                    value={member.label}
                    onSelect={() => add("user", member.user_id)}
                  >
                    <span className="min-w-0 flex-1 truncate">{member.label}</span>
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.intake_source_person)}
                    </span>
                  </CommandItem>
                ))}
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}
