"use client";

import { BookOpen, CircleHelp } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useConfigStore } from "@multica/core/config";
import { useT } from "../i18n";
import { docsHref } from "../common/docs-locale";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";

export function HelpLauncher() {
  const { t, i18n } = useT("layout");
  const serverVersion = useConfigStore((state) => state.serverVersion);
  return (
    <DropdownMenu>
      <TooltipHint label={t(($) => $.help.trigger)} side="right">
        <DropdownMenuTrigger
          aria-label={t(($) => $.help.trigger)}
          className={RAIL_ICON_BUTTON_CLASS}
        >
          <CircleHelp />
        </DropdownMenuTrigger>
      </TooltipHint>
      <DropdownMenuContent
        align="end"
        side="right"
        sideOffset={8}
        className="min-w-40 max-w-56"
      >
        {/* Same-origin docs. A plain anchor (no target) keeps the docs app's
            full-page render instead of routing it through the SPA. */}
        <DropdownMenuItem
          render={
            <a href={docsHref(i18n.language)} />
          }
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
        </DropdownMenuItem>
        {serverVersion && (
          <>
            <DropdownMenuSeparator />
            {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
                a Menu.Group context and throws if it has no Group ancestor. It
                must always be wrapped in a DropdownMenuGroup — without it the
                Help menu crashes the whole app on open (no error boundary sits
                above the rail). */}
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal break-words">
                {t(($) => $.help.server_version, { version: serverVersion })}
              </DropdownMenuLabel>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
