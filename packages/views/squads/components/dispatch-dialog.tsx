"use client";

import { useState } from "react";
import { useCreateIssue } from "@multica/core/issues/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

export type DispatchTarget = {
  type: "squad" | "agent";
  id: string;
  name: string;
};

const MAX_TITLE_LENGTH = 200;

/**
 * Hand work to a squad or an agent. The four-part handoff the server briefing
 * asks leaders for starts here: a dispatch that cannot state its goal and
 * acceptance criteria is not ready to send.
 */
export function DispatchDialog({
  target,
  open,
  onOpenChange,
}: {
  target: DispatchTarget;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("squads");
  const paths = useWorkspacePaths();
  const router = useNavigation();
  const createIssue = useCreateIssue();
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState("");
  const [input, setInput] = useState("");
  const [acceptance, setAcceptance] = useState("");
  const [boundary, setBoundary] = useState("");
  const [error, setError] = useState("");

  const ready = title.trim().length > 0 && title.trim().length <= MAX_TITLE_LENGTH && goal.trim() !== "" && acceptance.trim() !== "";

  const reset = () => {
    setTitle("");
    setGoal("");
    setInput("");
    setAcceptance("");
    setBoundary("");
    setError("");
  };

  const close = (next: boolean) => {
    if (createIssue.isPending) return;
    if (!next) reset();
    onOpenChange(next);
  };

  const submit = async () => {
    if (!ready) {
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
      const issue = await createIssue.mutateAsync({
        title: title.trim(),
        description,
        status: "todo",
        assignee_type: target.type,
        assignee_id: target.id,
      });
      reset();
      onOpenChange(false);
      router.push(paths.issueDetail(issue.id));
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.dispatch.failed));
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-lg" data-testid="dispatch-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.dispatch.title, { name: target.name })}</DialogTitle>
        </DialogHeader>
        <p className="text-caption text-muted-foreground">
          {target.type === "squad"
            ? t(($) => $.dispatch.squad_hint)
            : t(($) => $.dispatch.agent_hint)}
        </p>
        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto pr-1">
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
          <Button variant="outline" size="sm" onClick={() => close(false)} disabled={createIssue.isPending}>
            {t(($) => $.dispatch.cancel)}
          </Button>
          <Button size="sm" disabled={createIssue.isPending || !ready} onClick={() => void submit()}>
            {createIssue.isPending ? t(($) => $.dispatch.submitting) : t(($) => $.dispatch.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
