"use client";

import { useMemo, type ReactNode } from "react";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useT } from "../../i18n";
import {
  HealthDot,
  ProviderChip,
  machineIcon,
  shortDaemonId,
} from "./shared";
import { runtimeRowLabel, type RuntimeMachine } from "./runtime-machines";

export interface TeamRuntimeDrawerProps {
  /** Machine to inspect; `null` keeps the drawer closed. */
  machine: RuntimeMachine | null;
  agents: Agent[];
  members: MemberWithUser[];
  currentUserId: string | null;
  workloadByRuntimeId: ReadonlyMap<
    string,
    { runningCount: number; queuedCount: number }
  >;
  now: number;
  onClose: () => void;
}

/**
 * Read-only inspector for machines a department lead can see but not open.
 * The runtime detail page stays off-limits for these rows — the server's
 * per-runtime read gate answers 404 to non-owners — so the list exposes the
 * status metadata it already received (health, owner, bound agents) here.
 */
export function TeamRuntimeDrawer({
  machine,
  agents,
  members,
  currentUserId,
  workloadByRuntimeId,
  now,
  onClose,
}: TeamRuntimeDrawerProps) {
  const { t } = useT("runtimes");

  const ownerNames = useMemo(() => {
    if (!machine) return [];
    const ids = new Set(
      machine.runtimes
        .map((runtime) => runtime.owner_id)
        .filter((id): id is string => !!id),
    );
    return Array.from(ids).map((id) => {
      if (id === currentUserId) return t(($) => $.page.rows.owner_you);
      return (
        members.find((member) => member.user_id === id)?.name ??
        t(($) => $.page.team.unknown_owner)
      );
    });
  }, [machine, members, currentUserId, t]);

  const boundAgents = useMemo(() => {
    if (!machine) return [];
    const runtimeIds = new Set(machine.runtimes.map((runtime) => runtime.id));
    return agents.filter(
      (agent) => !agent.archived_at && agent.runtime_id && runtimeIds.has(agent.runtime_id),
    );
  }, [machine, agents]);

  const Icon = machine ? machineIcon(machine) : null;

  return (
    <Sheet open={machine !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 overflow-y-auto sm:max-w-md">
        {machine && Icon ? (
          <>
            <SheetHeader className="gap-1 border-b p-5">
              <SheetTitle className="flex items-center gap-2 text-title">
                <Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{machine.title}</span>
                <Badge variant="secondary" className="shrink-0">
                  {t(($) => $.page.rows.read_only)}
                </Badge>
              </SheetTitle>
              {machine.deviceSummary ? (
                <SheetDescription>{machine.deviceSummary}</SheetDescription>
              ) : null}
            </SheetHeader>

            <div className="flex flex-col gap-6 p-5">
              <p className="rounded-md border bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
                {t(($) => $.page.team.read_only_notice)}
              </p>

              <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
                <Field label={t(($) => $.page.team.owner)}>
                  {ownerNames.join(", ") || t(($) => $.page.team.unknown_owner)}
                </Field>
                <Field label={t(($) => $.page.team.health)}>
                  <span className="inline-flex items-center gap-1.5">
                    <HealthDot health={machine.health} />
                    {t(($) => $.health[machine.health].label)}
                  </span>
                </Field>
                {machine.deviceInfo ? (
                  <Field label={t(($) => $.page.team.system)}>{machine.deviceInfo}</Field>
                ) : null}
                {machine.daemonId ? (
                  <Field label={t(($) => $.page.team.daemon)} mono>
                    {shortDaemonId(machine.daemonId)}
                  </Field>
                ) : null}
                {machine.cliVersion ? (
                  <Field label={t(($) => $.page.team.cli_version)}>
                    {machine.cliVersion}
                  </Field>
                ) : null}
              </dl>

              <section className="flex flex-col gap-2">
                <h3 className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.page.team.runtimes)}
                </h3>
                <ul className="flex flex-col divide-y rounded-md border">
                  {machine.runtimes.map((runtime) => (
                    <li
                      key={runtime.id}
                      className="flex items-center gap-2 px-3 py-2"
                    >
                      <HealthDot health={deriveRuntimeHealth(runtime, now)} />
                      <span className="min-w-0 flex-1 truncate text-body">
                        {runtimeRowLabel(runtime, machine.title)}
                      </span>
                      <ProviderChip provider={runtime.provider} />
                    </li>
                  ))}
                </ul>
              </section>

              <section className="flex flex-col gap-2">
                <h3 className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.page.team.agents)}
                </h3>
                {boundAgents.length > 0 ? (
                  <ul className="flex flex-col divide-y rounded-md border">
                    {boundAgents.map((agent) => {
                      const workload = agent.runtime_id
                        ? workloadByRuntimeId.get(agent.runtime_id)
                        : undefined;
                      return (
                        <li key={agent.id} className="flex items-center gap-2 px-3 py-2">
                          <span className="min-w-0 flex-1 truncate text-body">
                            {agent.name}
                          </span>
                          <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
                            {workload && workload.runningCount + workload.queuedCount > 0
                              ? t(($) => $.page.team.workload, {
                                  running: workload.runningCount,
                                  queued: workload.queuedCount,
                                })
                              : t(($) => $.page.team.idle)}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.page.team.no_agents)}
                  </p>
                )}
              </section>
            </div>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function Field({
  label,
  mono,
  children,
}: {
  label: string;
  mono?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-caption text-muted-foreground">{label}</dt>
      <dd
        className={`mt-0.5 truncate text-body ${mono ? "font-mono text-caption" : ""}`}
      >
        {children}
      </dd>
    </div>
  );
}
