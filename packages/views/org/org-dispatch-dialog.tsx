"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EMPTY_ORG_TREE, api, type OrgTree } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueKeys } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { orgTreeOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { unitPaths, unitSubtreeIDs } from "./org-tree";

const MAX_TITLE_LENGTH = 200;

export interface OrgDispatchTarget {
  id: string;
  path: string;
  leader: string;
  agentCount: number;
  dispatchable: boolean;
}

/**
 * Departments the requester can see, with the leader the server would pick:
 * an existing squad's leader first, otherwise the department lead's twin, then
 * the first agent in the subtree. Units without an agent cannot lead work, so
 * they stay visible but unselectable.
 */
export function orgDispatchTargets(tree: OrgTree): OrgDispatchTarget[] {
  const paths = unitPaths(tree.units);
  return tree.units.map((unit) => {
    const subtree = unitSubtreeIDs(tree.units, unit.id);
    const agents = tree.agents.filter((agent) => agent.unit_id && subtree.has(agent.unit_id));
    const squad = tree.squads.find((candidate) => candidate.unit_id === unit.id);
    const leadTwin = unit.lead_user_id
      ? tree.agents.find((agent) => agent.owner_id === unit.lead_user_id)
      : undefined;
    const leaderID = squad?.leader_id || leadTwin?.id || agents[0]?.id || "";
    const leader = tree.agents.find((agent) => agent.id === leaderID)?.name ?? "";
    // An existing squad may lead even when its leader sits outside the subtree;
    // its member count is then the honest headcount.
    const agentCount = Math.max(agents.length, squad?.agent_count ?? 0);
    return {
      id: unit.id,
      path: paths.get(unit.id) ?? unit.name,
      leader,
      agentCount,
      dispatchable: agentCount > 0,
    };
  });
}

/**
 * The app-wide dispatch entry: pick a department, describe the outcome and how
 * it will be accepted, and the department's squad leader decomposes the work
 * and hands it to the team's agents. Completion comes back as an inbox item.
 */
export function OrgDispatchDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("squads");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const router = useNavigation();
  const queryClient = useQueryClient();
  const tree = useQuery(orgTreeOptions(wsId));
  const targets = useMemo(() => orgDispatchTargets(tree.data ?? EMPTY_ORG_TREE), [tree.data]);
  const available = targets.filter((target) => target.dispatchable);
  const [unitId, setUnitId] = useState("");
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState("");
  const [input, setInput] = useState("");
  const [acceptance, setAcceptance] = useState("");
  const [boundary, setBoundary] = useState("");
  const [error, setError] = useState("");

  // Prefer the requester's own department when it can lead; otherwise offer
  // the first usable one. Selection stays a controlled value, so a tree refresh
  // that removes the chosen unit falls back instead of submitting a stale id.
  const selected =
    targets.find((target) => target.id === unitId) ??
    targets.find((target) => target.id === tree.data?.my_unit_id && target.dispatchable) ??
    available[0] ??
    null;

  const itemLabels = new Map(
    targets.map((target) => [
      target.id,
      target.dispatchable
        ? target.leader
          ? `${target.path} · ${t(($) => $.dispatch.unit_leader, { name: target.leader })}`
          : target.path
        : `${target.path} · ${t(($) => $.dispatch.unit_no_agents)}`,
    ]),
  );
  const items = targets.map((target) => ({
    value: target.id,
    label: itemLabels.get(target.id) ?? target.path,
    disabled: !target.dispatchable,
  }));

  const ready =
    selected !== null &&
    title.trim().length > 0 &&
    title.trim().length <= MAX_TITLE_LENGTH &&
    goal.trim() !== "" &&
    acceptance.trim() !== "";

  const dispatch = useMutation({
    mutationFn: (input: { title: string; description: string }) => {
      if (!selected) throw new Error();
      return api.dispatchToOrgUnit(selected.id, input);
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
      await queryClient.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });

  const reset = () => {
    setUnitId("");
    setTitle("");
    setGoal("");
    setInput("");
    setAcceptance("");
    setBoundary("");
    setError("");
  };

  const close = (next: boolean) => {
    if (dispatch.isPending) return;
    if (!next) reset();
    onOpenChange(next);
  };

  const submit = async () => {
    if (!ready || !selected) {
      setError(t(($) => $.dispatch.required));
      return;
    }
    setError("");
    const sections = [
      [t(($) => $.dispatch.section_goal), goal.trim()],
      [t(($) => $.dispatch.section_input), input.trim()],
      [t(($) => $.dispatch.section_acceptance), acceptance.trim()],
      [t(($) => $.dispatch.section_boundary), boundary.trim()],
    ].filter(([, body]) => body !== "");
    const description = sections.map(([heading, body]) => `## ${heading}\n${body}`).join("\n\n");
    try {
      const issue = await dispatch.mutateAsync({ title: title.trim(), description });
      reset();
      onOpenChange(false);
      router.push(paths.issueDetail(issue.id));
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.dispatch.failed));
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-lg" data-testid="org-dispatch-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.dispatch.org_title)}</DialogTitle>
        </DialogHeader>
        <p className="text-caption text-muted-foreground">{t(($) => $.dispatch.org_hint)}</p>
        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto pr-1">
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.unit_label)}</span>
            {items.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.dispatch.unit_empty)}</p>
            ) : available.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.dispatch.unit_none_available)}</p>
            ) : (
              <Select
                items={items}
                value={selected?.id ?? ""}
                onValueChange={(next) => setUnitId(next ?? "")}
              >
                <SelectTrigger className="w-full">
                  <SelectValue>
                    {() =>
                      (selected && itemLabels.get(selected.id)) ??
                      t(($) => $.dispatch.unit_placeholder)
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {items.map((item) => (
                    <SelectItem key={item.value} value={item.value} disabled={item.disabled}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.title_label)}</span>
            <Input
              value={title}
              maxLength={MAX_TITLE_LENGTH}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={t(($) => $.dispatch.title_placeholder)}
              autoFocus
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.goal_label)}</span>
            <Textarea
              value={goal}
              onChange={(event) => setGoal(event.target.value)}
              placeholder={t(($) => $.dispatch.goal_placeholder)}
              className="min-h-16 resize-y"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.input_label)}</span>
            <Textarea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder={t(($) => $.dispatch.input_placeholder)}
              className="min-h-14 resize-y"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.acceptance_label)}</span>
            <Textarea
              value={acceptance}
              onChange={(event) => setAcceptance(event.target.value)}
              placeholder={t(($) => $.dispatch.acceptance_placeholder)}
              className="min-h-14 resize-y"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.dispatch.boundary_label)}</span>
            <Textarea
              value={boundary}
              onChange={(event) => setBoundary(event.target.value)}
              placeholder={t(($) => $.dispatch.boundary_placeholder)}
              className="min-h-14 resize-y"
            />
          </label>
        </div>
        {error ? <p className="text-body text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => close(false)} disabled={dispatch.isPending}>
            {t(($) => $.dispatch.cancel)}
          </Button>
          <Button size="sm" disabled={dispatch.isPending || !ready} onClick={() => void submit()}>
            {dispatch.isPending ? t(($) => $.dispatch.org_submitting) : t(($) => $.dispatch.org_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
