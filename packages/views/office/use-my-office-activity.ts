"use client";

import { useMemo } from "react";
import type { AgentTask } from "@multica/core/types";
import { useT } from "../i18n";
import { agentActivity, agentActivityText } from "./use-agent-activity";
import type { AgentDeskStatus } from "./scene/characters";

const EMPTY_TASKS: AgentTask[] = [];

/**
 * Head-bubble text for the C-end office, where the room shows only the agents
 * working on the viewer's own dispatches. Unlike the workspace office, it may
 * not name other people's issues, so labels and tasks both come from the
 * caller's dispatch subtree.
 */
export function useMyOfficeActivityText({
  agents,
  tasksByIssue,
  labels,
}: {
  agents: readonly { id: string; status: AgentDeskStatus; queuedCount: number }[];
  tasksByIssue: ReadonlyMap<string, AgentTask[]>;
  labels: ReadonlyMap<string, string>;
}) {
  const { t } = useT("office");
  return useMemo(() => {
    const runningByAgent = new Map<string, AgentTask[]>();
    for (const tasks of tasksByIssue.values()) {
      for (const task of tasks) {
        if (task.status !== "running") continue;
        const list = runningByAgent.get(task.agent_id);
        if (list) list.push(task);
        else runningByAgent.set(task.agent_id, [task]);
      }
    }
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
  }, [agents, labels, tasksByIssue, t]);
}
