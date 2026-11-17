"use client";

import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import {
  operatorDirectoryOptions,
  operatorKeys,
  operatorOverviewOptions,
  type OperatorDirectoryWorkspace,
  type OperatorWorkspaceOverview,
} from "@multica/core/operator";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { ArrowLeft, RefreshCw } from "@multica/ui/icons";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { formatTokens, formatUsd } from "../../runtimes/utils";

// Server ticks are 1e-10 USD (see task_usage_hourly.cost_usd_ticks).
const USD_TICKS_PER_DOLLAR = 10_000_000_000;

function rememberedWorkspaceSlug(): string {
  if (typeof document === "undefined") return "";
  const match = document.cookie.match(/(?:^|; )last_workspace_slug=([^;]*)/);
  return match?.[1] ? decodeURIComponent(match[1]) : "";
}

/** Where "back to workspace" lands. The operator can open every workspace,
 *  so this is the one they used last, or the first one in the console. */
function useOperatorHome(workspaces: Array<{ slug: string }>): string | null {
  const [remembered, setRemembered] = useState("");
  useEffect(() => {
    setRemembered(rememberedWorkspaceSlug());
  }, []);
  const slug =
    (remembered && workspaces.some((workspace) => workspace.slug === remembered)
      ? remembered
      : workspaces[0]?.slug) ?? "";
  return slug ? paths.workspace(slug).workbench() : null;
}

export function AdminPage() {
  const { t } = useT("admin");
  const overview = useQuery(operatorOverviewOptions());
  const workspaces = overview.data?.workspaces ?? [];
  const totals = summarize(workspaces);
  const home = useOperatorHome(workspaces);

  return (
    <div className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-8">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            {home ? (
              <AppLink
                href={home}
                className="inline-flex items-center gap-1 text-caption text-muted-foreground hover:text-foreground"
              >
                <ArrowLeft className="size-3.5" />
                {t(($) => $.page.back)}
              </AppLink>
            ) : (
              <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                <ArrowLeft className="size-3.5" />
                {t(($) => $.page.back)}
              </span>
            )}
            <h1 className="text-title-lg">{t(($) => $.page.title)}</h1>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void overview.refetch()}
            disabled={overview.isFetching}
          >
            <RefreshCw className={overview.isFetching ? "animate-spin" : undefined} />
            {t(($) => $.page.refresh)}
          </Button>
        </header>

        {overview.isError ? (
          <Alert variant="destructive">
            <AlertTitle>{t(($) => $.page.loadFailed)}</AlertTitle>
            <AlertDescription>
              <Button variant="outline" size="sm" onClick={() => void overview.refetch()}>
                {t(($) => $.page.retry)}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}

        <section className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-5">
          <SummaryCard label={t(($) => $.summary.workspaces)} value={String(totals.workspaces)} />
          <SummaryCard label={t(($) => $.summary.members)} value={totals.members.toLocaleString()} />
          <SummaryCard label={t(($) => $.summary.agents)} value={totals.agents.toLocaleString()} />
          <SummaryCard label={t(($) => $.summary.activeBots)} value={totals.activeBots.toLocaleString()} />
          <SummaryCard
            label={t(($) => $.summary.cost7d)}
            value={formatUsd(totals.costUsd)}
            hint={formatTokens(totals.tokens)}
          />
        </section>

        <DirectorySection workspaces={workspaces} />

        {overview.isPending ? (
          <Skeleton className="h-64 w-full" />
        ) : workspaces.length === 0 ? (
          <p className="text-body text-muted-foreground">{t(($) => $.page.empty)}</p>
        ) : (
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t(($) => $.table.workspace)}</TableHead>
                    <TableHead>{t(($) => $.table.owners)}</TableHead>
                    <TableHead>{t(($) => $.table.members)}</TableHead>
                    <TableHead>{t(($) => $.table.agents)}</TableHead>
                    <TableHead>{t(($) => $.table.channels)}</TableHead>
                    <TableHead>{t(($) => $.table.usage7d)}</TableHead>
                    <TableHead className="text-right">{t(($) => $.table.actions)}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {workspaces.map((workspace) => (
                    <WorkspaceRow key={workspace.id} workspace={workspace} />
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}

function DirectorySection({ workspaces }: { workspaces: OperatorWorkspaceOverview[] }) {
  const { t } = useT("admin");
  const queryClient = useQueryClient();
  const directories = useQuery(operatorDirectoryOptions());
  const [syncing, setSyncing] = useState<string | null>(null);
  const [failure, setFailure] = useState("");
  const rows = directories.data?.workspaces ?? workspaces.map((workspace) => ({
    id: workspace.id,
    name: workspace.name,
    slug: workspace.slug,
    unit_count: 0,
    placed_members: 0,
    last_synced_at: "",
    created_units: 0,
    updated_units: 0,
    members_added: 0,
    members_moved: 0,
  }));

  const run = async (workspace: OperatorDirectoryWorkspace) => {
    setFailure("");
    setSyncing(workspace.id);
    try {
      await api.syncOperatorDirectory(workspace.id);
      await queryClient.invalidateQueries({ queryKey: operatorKeys.directories() });
    } catch (error) {
      setFailure(error instanceof Error && error.message ? error.message : t(($) => $.directory.failed));
    } finally {
      setSyncing(null);
    }
  };

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-heading">{t(($) => $.directory.title)}</h2>
        <span className="text-caption text-muted-foreground">{t(($) => $.directory.hint)}</span>
      </div>
      {directories.data && !directories.data.configured ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.directory.notConfigured)}</p>
      ) : null}
      {failure ? <p className="text-body text-destructive">{failure}</p> : null}
      {directories.isError ? (
        <div className="flex items-center gap-3">
          <p className="text-body text-destructive">{t(($) => $.directory.loadFailed)}</p>
          <Button variant="outline" size="sm" onClick={() => void directories.refetch()}>
            {t(($) => $.page.retry)}
          </Button>
        </div>
      ) : null}
      {directories.isPending && rows.length === 0 ? (
        <Skeleton className="h-24 w-full" />
      ) : rows.length === 0 ? (
        <p className="text-body text-muted-foreground">{t(($) => $.directory.empty)}</p>
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.table.workspace)}</TableHead>
                  <TableHead>{t(($) => $.directory.units)}</TableHead>
                  <TableHead>{t(($) => $.directory.placed)}</TableHead>
                  <TableHead>{t(($) => $.directory.lastHeading)}</TableHead>
                  <TableHead className="text-right">{t(($) => $.table.actions)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((workspace) => (
                  <TableRow key={workspace.id}>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="font-medium">{workspace.name}</span>
                        <span className="text-caption text-muted-foreground">{workspace.slug}</span>
                      </div>
                    </TableCell>
                    <TableCell className="tabular-nums">{workspace.unit_count}</TableCell>
                    <TableCell className="tabular-nums">{workspace.placed_members}</TableCell>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="text-caption">
                          {workspace.last_synced_at
                            ? t(($) => $.directory.last, {
                                time: new Date(workspace.last_synced_at).toLocaleString(),
                              })
                            : t(($) => $.directory.never)}
                        </span>
                        {workspace.last_synced_at ? (
                          <span className="text-caption text-muted-foreground">
                            {t(($) => $.directory.report, {
                              created: workspace.created_units,
                              updated: workspace.updated_units,
                              added: workspace.members_added,
                              moved: workspace.members_moved,
                            })}
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={directories.data?.configured === false || syncing !== null}
                        onClick={() => void run(workspace)}
                      >
                        {syncing === workspace.id ? t(($) => $.directory.syncing) : t(($) => $.directory.sync)}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </section>
  );
}

function SummaryCard({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-1 py-4">
        <span className="text-caption text-muted-foreground">{label}</span>
        <span className="text-title-lg tabular-nums">{value}</span>
        {hint ? <span className="text-caption text-muted-foreground tabular-nums">{hint}</span> : null}
      </CardContent>
    </Card>
  );
}

function WorkspaceRow({ workspace }: { workspace: OperatorWorkspaceOverview }) {
  const { t } = useT("admin");
  const settingsBase = `/${workspace.slug}/settings`;
  const activeChannels = workspace.channels.filter((channel) => channel.active > 0);
  const revokedChannels = workspace.channels.reduce(
    (sum, channel) => sum + channel.revoked,
    0,
  );
  const channelLabels: Record<string, string> = {
    feishu: t(($) => $.channels.feishu),
    slack: t(($) => $.channels.slack),
    dingtalk: t(($) => $.channels.dingtalk),
    wecom: t(($) => $.channels.wecom),
    telegram: t(($) => $.channels.telegram),
  };
  const tokens =
    workspace.usage_7d.input_tokens +
    workspace.usage_7d.output_tokens +
    workspace.usage_7d.cache_read_tokens +
    workspace.usage_7d.cache_write_tokens;

  return (
    <TableRow>
      <TableCell>
        <div className="flex flex-col">
          <span className="font-medium">{workspace.name}</span>
          <span className="text-caption text-muted-foreground">{workspace.slug}</span>
        </div>
      </TableCell>
      <TableCell>
        {workspace.owner_emails.length === 0 ? (
          <span className="text-muted-foreground">{t(($) => $.table.none)}</span>
        ) : (
          <div className="flex flex-col">
            {workspace.owner_emails.map((email) => (
              <span key={email} className="text-caption">{email}</span>
            ))}
          </div>
        )}
      </TableCell>
      <TableCell className="tabular-nums">
        <div className="flex flex-col">
          <span>
            {t(($) => $.table.roleBreakdown, {
              owners: workspace.owner_count,
              admins: workspace.admin_count,
              members: workspace.member_count,
            })}
          </span>
        </div>
      </TableCell>
      <TableCell className="tabular-nums">{workspace.agent_count.toLocaleString()}</TableCell>
      <TableCell>
        {activeChannels.length === 0 && revokedChannels === 0 ? (
          <span className="text-muted-foreground">{t(($) => $.table.none)}</span>
        ) : (
          <div className="flex flex-wrap items-center gap-1">
            {activeChannels.map((channel) => (
              <Badge key={channel.type} variant="secondary">
                {channelLabels[channel.type] ?? channel.type} {channel.active}
              </Badge>
            ))}
            {revokedChannels > 0 ? (
              <span className="text-caption text-muted-foreground">
                {t(($) => $.table.revokedCount, { revoked: revokedChannels })}
              </span>
            ) : null}
          </div>
        )}
      </TableCell>
      <TableCell className="tabular-nums">
        <div className="flex flex-col">
          <span>{formatUsd(workspace.usage_7d.cost_usd_ticks / USD_TICKS_PER_DOLLAR)}</span>
          <span className="text-caption text-muted-foreground">
            {t(($) => $.table.usageBreakdown, {
              tasks: workspace.usage_7d.task_count.toLocaleString(),
              tokens: formatTokens(tokens),
            })}
          </span>
        </div>
      </TableCell>
      <TableCell className="text-right">
        <div className="flex justify-end gap-2">
          <AppLink
            href={`${settingsBase}?tab=channels`}
            className="text-caption text-primary hover:underline"
          >
            {t(($) => $.table.channelsAction)}
          </AppLink>
          <AppLink
            href={`${settingsBase}?tab=members`}
            className="text-caption text-primary hover:underline"
          >
            {t(($) => $.table.membersAction)}
          </AppLink>
          <AppLink
            href={`/${workspace.slug}/usage`}
            className="text-caption text-primary hover:underline"
          >
            {t(($) => $.table.usageAction)}
          </AppLink>
        </div>
      </TableCell>
    </TableRow>
  );
}

function summarize(workspaces: OperatorWorkspaceOverview[]) {
  return workspaces.reduce(
    (acc, workspace) => ({
      workspaces: acc.workspaces + 1,
      members: acc.members + workspace.owner_count + workspace.admin_count + workspace.member_count,
      agents: acc.agents + workspace.agent_count,
      activeBots: acc.activeBots + workspace.channels.reduce((sum, c) => sum + c.active, 0),
      costUsd: acc.costUsd + workspace.usage_7d.cost_usd_ticks / USD_TICKS_PER_DOLLAR,
      tokens:
        acc.tokens +
        workspace.usage_7d.input_tokens +
        workspace.usage_7d.output_tokens +
        workspace.usage_7d.cache_read_tokens +
        workspace.usage_7d.cache_write_tokens,
    }),
    { workspaces: 0, members: 0, agents: 0, activeBots: 0, costUsd: 0, tokens: 0 },
  );
}
