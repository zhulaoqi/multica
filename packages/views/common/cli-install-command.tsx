"use client";

import { type ReactNode, useSyncExternalStore } from "react";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { useAppOrigin } from "../navigation";

/**
 * The Multica CLI install commands, one per platform family.
 *
 * Three surfaces render install instructions — the runtimes "add computer"
 * dialog, onboarding's CLI card, and the landing download page. They read the
 * commands from here instead of each hardcoding a string, which is how Windows
 * silently went missing from all of them while the PowerShell installer sat in
 * the repo. The deployment's web app serves both scripts, so the command only
 * needs the origin it is shown on; nothing points at a public code host.
 */
const INSTALL_PATHS = {
  macosLinux: "/install/install.sh",
  windows: "/install/install.ps1",
} as const;

export type CliInstallPlatform = "macosLinux" | "macos" | "linux" | "windows";

/** Shell command that installs the CLI from the given deployment origin. */
export function cliInstallCommands(
  origin: string,
): Record<keyof typeof INSTALL_PATHS, string> {
  const base = origin.replace(/\/+$/, "");
  return {
    macosLinux: `curl -fsSL ${base}${INSTALL_PATHS.macosLinux} | bash`,
    windows: `irm ${base}${INSTALL_PATHS.windows} | iex`,
  };
}

function subscribeToOriginChange(): () => void {
  return () => {};
}

/**
 * Origin the commands must target. On the web that is the page's own origin;
 * inside the desktop app the renderer is loaded from `file://`, so the
 * connected environment's app URL comes from the platform adapter instead.
 * `useSyncExternalStore` keeps the server render and the hydrated render from
 * disagreeing about the first value.
 */
function currentOrigin(appOrigin: string | null): string {
  if (typeof window === "undefined") return "";
  if (window.location.protocol === "file:") return appOrigin ?? "";
  return window.location.origin;
}

/** Tab order. macOS / Linux stays first so the default matches the old single command. */
const PLATFORMS = [
  "macosLinux",
  "windows",
] as const satisfies readonly CliInstallPlatform[];
const SPLIT_UNIX_PLATFORMS = [
  "macos",
  "linux",
  "windows",
] as const satisfies readonly CliInstallPlatform[];

export interface CliInstallCommandLabels {
  /** Accessible name for the platform switch. */
  group: string;
  macosLinux: string;
  macos?: string;
  linux?: string;
  windows: string;
}

/**
 * Platform switch for the install command, built on the shared Tabs primitive
 * so tab semantics and arrow-key navigation come for free.
 *
 * The switch is the only shared UI: `children` receives the selected command
 * and renders it, so each surface keeps its own command row — the product
 * surfaces their token-styled row with an icon copy button, the landing page
 * its marketing palette with a spelled-out one.
 */
export function CliInstallCommand({
  labels,
  classNames,
  separateUnix = false,
  onPlatformChange,
  children,
}: {
  labels: CliInstallCommandLabels;
  /** Palette overrides for surfaces that do not use the product tokens. */
  classNames?: { list?: string; trigger?: string };
  /** Shows macOS and Linux separately when their setup flows differ. */
  separateUnix?: boolean;
  onPlatformChange?: (platform: CliInstallPlatform) => void;
  children: (command: string) => ReactNode;
}) {
  const appOrigin = useAppOrigin();
  const origin = useSyncExternalStore(
    subscribeToOriginChange,
    () => currentOrigin(appOrigin),
    () => "",
  );
  const commands = cliInstallCommands(origin);
  const platforms = separateUnix ? SPLIT_UNIX_PLATFORMS : PLATFORMS;

  return (
    <Tabs
      defaultValue={platforms[0]}
      onValueChange={(value) => {
        onPlatformChange?.(value as CliInstallPlatform);
      }}
    >
      <TabsList
        aria-label={labels.group}
        activateOnFocus
        className={classNames?.list}
      >
        {platforms.map((platform) => (
          <TabsTrigger
            key={platform}
            value={platform}
            className={classNames?.trigger}
          >
            {labels[platform] ?? platform}
          </TabsTrigger>
        ))}
      </TabsList>
      {platforms.map((platform) => (
        <TabsContent key={platform} value={platform}>
          {children(
            commands[
              platform === "windows" ? "windows" : "macosLinux"
            ],
          )}
        </TabsContent>
      ))}
    </Tabs>
  );
}
