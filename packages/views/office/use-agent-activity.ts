"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { useT } from "../i18n";
import type { AgentDeskStatus } from "./scene/characters";

const EMPTY_TASKS: AgentTask[] = [];

type OfficeT = ReturnType<typeof useT<"office">>["t"];

export interface AgentActivityInput {
  status: AgentDeskStatus;
  /** Tasks the agent is running right now. */
  running: readonly AgentTask[];
  queuedCount: number;
  /** `IDENT title` for each loaded issue, keyed by issue id. */
  issueLabels: ReadonlyMap<string, string>;
}

export type AgentActivity =
  | { kind: "issue"; label: string }
  | { kind: "running"; count: number }
  | { kind: "chat" }
  | { kind: "autopilot" }
  | { kind: "queued"; count: number }
  | { kind: "working" | "idle" | "offline" };

/**
 * What an agent's head bubble says. A single running issue names the issue;
 * several runs read as a count, since one caption cannot name them all. Runs
 * without an issue are chats or autopilots. Everyone else shows their status
 * so every agent in the room carries a bubble.
 */
export function agentActivity(input: AgentActivityInput): AgentActivity {
  if (input.status === "offline") return { kind: "offline" };
  if (input.status === "idle") return { kind: "idle" };
  if (input.status === "queued") return { kind: "queued", count: input.queuedCount };
  if (input.running.length > 1) return { kind: "running", count: input.running.length };
  const task = input.running[0];
  if (!task) return { kind: "working" };
  if (task.issue_id) {
    const label = input.issueLabels.get(task.issue_id);
    return label ? { kind: "issue", label } : { kind: "working" };
  }
  if (task.chat_session_id || task.kind === "chat") return { kind: "chat" };
  if (task.autopilot_run_id || task.kind === "autopilot") return { kind: "autopilot" };
  return { kind: "working" };
}

/**
 * Head-bubble text per agent id. Issue titles come from the issue detail cache
 * the rest of the app shares, so a bubble names the same issue the Team panel
 * does.
 */
export function useAgentActivityText(
  wsId: string,
  agents: readonly { id: string; status: AgentDeskStatus; queuedCount: number }[],
): Map<string, string> {
  const { t } = useT("office");
  const { data: snapshot = EMPTY_TASKS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: !!wsId,
  });

  const runningByAgent = useMemo(() => {
    const map = new Map<string, AgentTask[]>();
    for (const task of snapshot) {
      if (task.status !== "running") continue;
      const list = map.get(task.agent_id);
      if (list) list.push(task);
      else map.set(task.agent_id, [task]);
    }
    return map;
  }, [snapshot]);

  // Only agents on exactly one issue run name it, so only those are fetched.
  const issueIds = useMemo(() => {
    const ids = new Set<string>();
    for (const tasks of runningByAgent.values()) {
      if (tasks.length === 1 && tasks[0]!.issue_id) ids.add(tasks[0]!.issue_id);
    }
    return [...ids].sort();
  }, [runningByAgent]);

  const issueLabels = useQueries({
    queries: issueIds.map((id) => ({
      ...issueDetailOptions(wsId, id),
      enabled: !!wsId,
      staleTime: 60_000,
      retry: false,
    })),
    combine: (results) =>
      results.flatMap(({ data }) =>
        data?.id && data.identifier ? [[data.id, `${data.identifier} ${data.title}`] as const] : [],
      ),
  });

  return useMemo(() => {
    const labels = new Map(issueLabels);
    const texts = new Map<string, string>();
    for (const agent of agents) {
      const activity = agentActivity({
        status: agent.status,
        running: runningByAgent.get(agent.id) ?? EMPTY_TASKS,
        queuedCount: agent.queuedCount,
        issueLabels: labels,
      });
      texts.set(agent.id, agentActivityText(activity, t));
    }
    return texts;
  }, [agents, runningByAgent, issueLabels, t]);
}

export function agentActivityText(activity: AgentActivity, t: OfficeT): string {
  switch (activity.kind) {
    case "issue":
      return activity.label;
    case "running":
      return t(($) => $.page.activity.running, { count: activity.count });
    case "queued":
      return activity.count > 0
        ? t(($) => $.page.activity.queued, { count: activity.count })
        : t(($) => $.page.status.queued);
    case "chat":
      return t(($) => $.page.activity.chat);
    case "autopilot":
      return t(($) => $.page.activity.autopilot);
    case "working":
      return t(($) => $.page.activity.working);
    case "idle":
      return t(($) => $.page.activity.idle);
    case "offline":
      return t(($) => $.page.activity.offline);
  }
}
