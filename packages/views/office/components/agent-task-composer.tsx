"use client";

import { useCallback, useState } from "react";
import { Loader2, Send } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { api, ApiError } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { useT } from "../../i18n";

export interface AgentTaskComposerProps {
  agent: Agent;
  /** The server accepted the task; the panel returns to its action row. */
  onCreated: () => void;
  /** Hands the typed brief to the full create dialog. */
  onMoreOptions: (prompt: string) => void;
  onCancel: () => void;
}

function taskErrorMessage(
  error: unknown,
  copy: { fallback: string; unavailable: string; limit: string },
): string {
  if (error instanceof ApiError && error.body && typeof error.body === "object") {
    const body = error.body as { code?: string; reason?: string };
    if (body.code === "agent_unavailable") {
      return body.reason || copy.unavailable;
    }
    if (body.code === "issue_limit_reached") {
      return copy.limit;
    }
  }
  return copy.fallback;
}

/**
 * Inline brief for assigning work from the office. Sends the same quick-create
 * call as the modal, and hands the text to the modal when the user needs the
 * full form (project, priority, attachments).
 */
export function AgentTaskComposer({
  agent,
  onCreated,
  onMoreOptions,
  onCancel,
}: AgentTaskComposerProps) {
  const { t } = useT("office");
  const [prompt, setPrompt] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const trimmed = prompt.trim();

  const submit = useCallback(async () => {
    if (!trimmed || pending) return;
    setPending(true);
    setError(null);
    try {
      await api.quickCreateIssue({ agent_id: agent.id, prompt: trimmed });
      setPrompt("");
      onCreated();
    } catch (submitError) {
      setError(
        taskErrorMessage(submitError, {
          fallback: t(($) => $.page.interact.task_error),
          unavailable: t(($) => $.page.interact.task_error_agent_unavailable),
          limit: t(($) => $.page.interact.task_error_issue_limit),
        }),
      );
    } finally {
      setPending(false);
    }
  }, [agent.id, trimmed, pending, onCreated, t]);

  return (
    <form
      className="flex flex-col gap-2"
      aria-busy={pending}
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <Textarea
        autoFocus
        rows={2}
        value={prompt}
        disabled={pending}
        aria-label={t(($) => $.page.interact.task_placeholder)}
        placeholder={t(($) => $.page.interact.task_placeholder)}
        onChange={(event) => setPrompt(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            void submit();
          }
        }}
      />
      {error && (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        <Button type="submit" size="sm" disabled={!trimmed || pending} aria-busy={pending}>
          {pending ? <Loader2 className="animate-spin" /> : <Send />}
          {t(($) => $.page.interact.task_send)}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={pending}
          onClick={() => onMoreOptions(trimmed)}
        >
          {t(($) => $.page.interact.task_more_options)}
        </Button>
        <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={onCancel}>
          {t(($) => $.page.interact.cancel)}
        </Button>
      </div>
    </form>
  );
}
