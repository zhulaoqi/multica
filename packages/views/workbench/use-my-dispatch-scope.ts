"use client";

import { useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import {
  childIssueProgressOptions,
  childrenByParentsOptions,
} from "@multica/core/issues/queries";
import type { AgentTask, Issue } from "@multica/core/types";
import { squadListOptions } from "@multica/core/workspace/queries";
import { useMyDispatches, type DispatchListItem } from "@multica/core/workbench";

/**
 * The work the caller sent, flattened into the ids the C-end reads: the
 * dispatch roots, the tasks their leaders created, the agents those tasks
 * belong to, and the live runs on that work.
 *
 * One dispatch tree is: the root issue (visible to its creator), the direct
 * children the department lead decomposed it into, and every agent assigned
 * to those children or leading their squad. The C-end uses this to draw the
 * task board and the office without ever leaving the requester's own work.
 */
export interface MyDispatchScope {
  dispatches: DispatchListItem[];
  isLoading: boolean;
  error: Error | null;
  /** Direct children keyed by the dispatch root they decompose. */
  childrenByRoot: ReadonlyMap<string, Issue[]>;
  /** Roots plus their direct children. */
  issueIds: ReadonlySet<string>;
  /** Agents assigned to the work, directly or as a squad lead. */
  agentIds: ReadonlySet<string>;
  /** Agents with a running task on one of the issues right now. */
  workingAgentIds: ReadonlySet<string>;
  /** Tasks per issue, from the workspace task snapshot. */
  tasksByIssue: ReadonlyMap<string, AgentTask[]>;
  /** Completed / total children per dispatch root. */
  progressByRoot: ReadonlyMap<string, { done: number; total: number }>;
}

const NO_DISPATCHES: DispatchListItem[] = [];

export function useMyDispatchScope(wsId: string): MyDispatchScope {
  const { dispatches: allDispatches, isLoading, error } = useMyDispatches(wsId);
  const dispatches = allDispatches ?? NO_DISPATCHES;
  const qc = useQueryClient();

  const rootIds = useMemo(
    () => [...new Set(dispatches.map((item) => item.issue.id))].sort(),
    [dispatches],
  );

  const { data: childrenByRoot } = useQuery({
    ...childrenByParentsOptions(wsId, rootIds, qc),
    enabled: !!wsId && rootIds.length > 0,
  });

  const { data: squads = [] } = useQuery({
    ...squadListOptions(wsId),
    enabled: !!wsId,
  });

  const { data: tasks = [] } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: !!wsId,
  });

  const { data: progressByRoot } = useQuery({
    ...childIssueProgressOptions(wsId),
    enabled: !!wsId,
  });

  return useMemo(() => {
    const childrenMap = childrenByRoot ?? new Map<string, Issue[]>();
    const issueIds = new Set(rootIds);
    for (const children of childrenMap.values()) {
      for (const child of children) issueIds.add(child.id);
    }

    const leaderBySquad = new Map(
      squads.filter((squad) => !squad.archived_at).map((squad) => [squad.id, squad.leader_id]),
    );
    const agents = new Set<string>();
    const addAssignee = (type: string | null | undefined, id: string | null | undefined) => {
      if (!id) return;
      if (type === "agent") agents.add(id);
      else if (type === "squad") {
        const leader = leaderBySquad.get(id);
        if (leader) agents.add(leader);
      }
    };
    for (const item of dispatches) {
      addAssignee(item.issue.assignee_type, item.issue.assignee_id);
    }
    for (const children of childrenMap.values()) {
      for (const child of children) addAssignee(child.assignee_type, child.assignee_id);
    }

    const tasksByIssue = new Map<string, AgentTask[]>();
    const working = new Set<string>();
    for (const task of tasks) {
      if (!task.issue_id || !issueIds.has(task.issue_id)) continue;
      const bucket = tasksByIssue.get(task.issue_id);
      if (bucket) bucket.push(task);
      else tasksByIssue.set(task.issue_id, [task]);
      if (task.status === "running") {
        agents.add(task.agent_id);
        working.add(task.agent_id);
      }
    }
    for (const issueTasks of tasksByIssue.values()) {
      for (const task of issueTasks) agents.add(task.agent_id);
    }

    return {
      dispatches,
      isLoading,
      error,
      childrenByRoot: childrenMap,
      issueIds,
      agentIds: agents,
      workingAgentIds: working,
      tasksByIssue,
      progressByRoot: progressByRoot ?? new Map(),
    };
  }, [
    childrenByRoot,
    dispatches,
    error,
    isLoading,
    progressByRoot,
    rootIds,
    squads,
    tasks,
  ]);
}
