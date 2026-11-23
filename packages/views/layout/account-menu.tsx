"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Keyboard,
  Globe,
  LayoutDashboard,
  LogOut,
  Monitor,
  Moon,
  Palette,
  Shield,
  Sun,
  User,
  Users,
} from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { paths, useWorkspacePaths } from "@multica/core/paths";
import type { SupportedLocale } from "@multica/core/i18n";
import type { Workspace } from "@multica/core/types";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useLogout } from "../auth";
import { useLanguageSwitch } from "../common/use-language-switch";
import { useT } from "../i18n";
import { isDesktopShell } from "../platform";
import { AppLink } from "../navigation";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";

const THEMES = [
  { value: "light", icon: Sun },
  { value: "dark", icon: Moon },
  { value: "system", icon: Monitor },
] as const;

function SubmenuLabel({ label, value }: { label: string; value?: string }) {
  return (
    <>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {value && <span className="shrink-0 text-caption text-muted-foreground">{value}</span>}
    </>
  );
}

// Stable empty default: an inline `= []` is a new array every render.
const EMPTY_WORKSPACES: Workspace[] = [];

/**
 * The signed-in user's menu at the foot of the rail.
 *
 * `home` is the C-end variant: it never links into the current workspace's
 * settings. Managers reach the admin side through the organization's org page;
 * everyone else reaches the organization's team page, where they create and
 * bind their own agent. The workspace variant mirrors it on web with a link
 * back to `/home`; desktop has no C-end, so it shows neither.
 */
export function AccountMenu({ variant = "workspace" }: { variant?: "workspace" | "home" }) {
  const { t } = useT("layout");
  const user = useAuthStore((s) => s.user);
  const p = useWorkspacePaths();
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery({
    ...workspaceListOptions(),
    enabled: variant === "home",
  });
  // The organization is the workspace every member belongs to, so its entry is
  // the one the C-end offers: managers get its org page, everyone else its
  // team page to create their own agent.
  const homeEntry = useMemo(() => {
    if (variant !== "home") return null;
    const organization = workspaces.find((ws) => ws.is_organization);
    if (organization) {
      const targetPaths = paths.workspace(organization.slug);
      return organization.viewer_can_manage !== false
        ? ({ href: targetPaths.org(), label: "admin" } as const)
        : ({ href: targetPaths.agents(), label: "my_agent" } as const);
    }
    // Legacy data without an organization: keep the old manageable-workspace
    // fallback.
    const target = workspaces.find((ws) => ws.viewer_can_manage !== false);
    if (!target) return null;
    return { href: paths.workspace(target.slug).issues(), label: "admin" } as const;
  }, [variant, workspaces]);
  const logout = useLogout();
  const { theme, setTheme } = useTheme();
  const language = useLanguageSwitch();
  const themeValue = (theme ?? "system") as (typeof THEMES)[number]["value"];
  const name = user?.name ?? "";
  const avatar = (size: "md" | "lg") => (
    <ActorAvatar
      name={name}
      initials={(name || "U").charAt(0).toUpperCase()}
      avatarUrl={resolvePublicFileUrl(user?.avatar_url)}
      size={size}
    />
  );

  return (
    <DropdownMenu>
      <TooltipHint label={t(($) => $.account.trigger)} side="right">
        <DropdownMenuTrigger
          aria-label={t(($) => $.account.trigger)}
          className={cn(RAIL_ICON_BUTTON_CLASS, "rounded-full")}
        >
          {avatar("md")}
        </DropdownMenuTrigger>
      </TooltipHint>
      <DropdownMenuContent side="right" align="end" sideOffset={8} className="w-64">
        <div className="flex items-center gap-2.5 px-2 py-1.5">
          {avatar("lg")}
          <div className="min-w-0 flex-1">
            <p className="truncate text-body font-medium leading-tight">{user?.name}</p>
            <p className="truncate text-caption leading-tight text-muted-foreground">
              {user?.email}
            </p>
          </div>
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          {variant === "workspace" && (
            <DropdownMenuItem render={<AppLink href={`${p.settings()}?tab=profile`} />}>
              <User />
              {t(($) => $.account.profile)}
            </DropdownMenuItem>
          )}
          {variant === "workspace" ? (
            isDesktopShell() ? null : (
              <DropdownMenuItem render={<AppLink href={paths.home()} />}>
                <LayoutDashboard />
                {t(($) => $.account.cend)}
              </DropdownMenuItem>
            )
          ) : (
            homeEntry && (
              <DropdownMenuItem render={<AppLink href={homeEntry.href} />}>
                {homeEntry.label === "admin" ? <LayoutDashboard /> : <Users />}
                {homeEntry.label === "admin"
                  ? t(($) => $.account.admin)
                  : t(($) => $.account.my_agent)}
              </DropdownMenuItem>
            )
          )}
          {user?.is_operator && !isDesktopShell() ? (
            <DropdownMenuItem render={<AppLink href="/admin" />}>
              <Shield />
              {t(($) => $.account.console)}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Palette />
              <SubmenuLabel
                label={t(($) => $.account.theme)}
                value={t(($) => $.account.themes[themeValue])}
              />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup value={themeValue} onValueChange={setTheme}>
                {THEMES.map(({ value, icon: Icon }) => (
                  <DropdownMenuRadioItem key={value} value={value}>
                    <Icon />
                    {t(($) => $.account.themes[value])}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Globe />
              <SubmenuLabel
                label={t(($) => $.account.language)}
                value={language.options.find((o) => o.value === language.currentLocale)?.label}
              />
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={language.currentLocale}
                onValueChange={(next) => void language.changeLanguage(next as SupportedLocale)}
              >
                {language.options.map((option) => (
                  <DropdownMenuRadioItem key={option.value} value={option.value}>
                    {option.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          {variant === "workspace" && (
            <DropdownMenuItem render={<AppLink href={`${p.settings()}?tab=shortcuts`} />}>
              <Keyboard />
              {t(($) => $.account.shortcuts)}
            </DropdownMenuItem>
          )}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onClick={logout}>
            <LogOut />
            {t(($) => $.sidebar.log_out)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
