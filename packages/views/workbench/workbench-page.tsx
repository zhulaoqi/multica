"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Clock, Plus, Sparkles } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  parseDispatchCandidates,
  useDispatchWork,
  useMyDispatches,
  type DispatchCandidate,
  type DispatchListItem,
} from "@multica/core/workbench";
import { orgTreeOptions } from "@multica/core/workspace/queries";
import { createSafeId } from "@multica/core/utils";
import { CollectionPageHeader } from "../layout";
import { useT, useTimeAgo } from "../i18n";
import { useNavigation } from "../navigation";
import { unitPaths } from "../org/org-tree";
import { DispatchComposer } from "./dispatch-composer";
import {
  DispatchAttemptConversation,
  DispatchConversation,
  type WorkbenchAttempt,
} from "./dispatch-conversation";
import {
  useWorkbenchConversation,
  type DispatchPhase,
} from "./use-workbench-conversation";

function phaseDotClass(phase: DispatchPhase): string {
  switch (phase) {
    case "completed":
      return "bg-success";
    case "in_review":
      return "bg-warning";
    case "failed":
      return "bg-destructive";
    case "cancelled":
      return "bg-muted-foreground/50";
    case "queued":
      return "bg-muted-foreground/50";
    default:
      return "bg-brand";
  }
}

function HistoryList({
  dispatches,
  selectedId,
  isLoading,
  error,
  phaseFor,
  onSelect,
}: {
  dispatches: DispatchListItem[];
  selectedId: string | null;
  isLoading: boolean;
  error: Error | null;
  phaseFor: (item: DispatchListItem) => DispatchPhase;
  onSelect: (item: DispatchListItem) => void;
}) {
  const { t } = useT("workbench");
  const timeAgo = useTimeAgo();

  if (isLoading && dispatches.length === 0) {
    return (
      <div className="flex flex-col gap-3 p-2">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    );
  }
  if (error) {
    return (
      <p className="px-2 py-3 text-caption text-muted-foreground">
        {t(($) => $.page.load_error)}
      </p>
    );
  }
  if (dispatches.length === 0) {
    return (
      <p className="px-2 py-3 text-caption text-muted-foreground">
        {t(($) => $.page.empty_history)}
      </p>
    );
  }
  return (
    <ul className="flex flex-col gap-0.5">
      {dispatches.map((item) => {
        const selected = item.issue.id === selectedId;
        return (
          <li key={item.issue.id}>
            <button
              type="button"
              aria-current={selected ? "true" : undefined}
              onClick={() => onSelect(item)}
              className={cn(
                "flex w-full flex-col gap-1 rounded-md px-2 py-2 text-left transition-colors hover:bg-muted",
                selected && "bg-muted",
              )}
            >
              <span className="flex items-center gap-2">
                <span
                  aria-hidden
                  className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    phaseDotClass(phaseFor(item)),
                  )}
                />
                <span className="min-w-0 flex-1 truncate text-body">
                  {item.issue.title}
                </span>
              </span>
              <span className="min-w-0 truncate pl-3.5 text-caption text-muted-foreground">
                {item.unit_path || t(($) => $.page.unknown_department)}
                {" · "}
                {timeAgo(item.dispatched_at)}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * The conversational workbench: describe work in natural language and watch
 * the routed department decompose and deliver it in the same thread.
 *
 * The history is the server's dispatch_origin list, not local chat state, so a
 * refresh or another device shows the same threads. Only submissions the
 * server has not accepted yet live in component state.
 */
export function WorkbenchPage() {
  const { t } = useT("workbench");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { pathname, searchParams, push, replace } = useNavigation();
  const isCompact = useIsCompact();
  const workbenchPath = p.workbench();

  const { dispatches, isLoading, error } = useMyDispatches(wsId);
  const { phaseFor, progressFor } = useWorkbenchConversation(wsId);
  const dispatch = useDispatchWork(wsId);

  const selectedId = searchParams.get("dispatch");
  const presetUnitId = searchParams.get("unit");
  const selected = useMemo(
    () =>
      selectedId
        ? (dispatches.find((item) => item.issue.id === selectedId) ?? null)
        : null,
    [dispatches, selectedId],
  );

  const orgTree = useQuery({
    ...orgTreeOptions(wsId),
    enabled: !!presetUnitId,
  });
  const presetUnitPath = useMemo(() => {
    if (!presetUnitId) return null;
    return unitPaths(orgTree.data?.units ?? []).get(presetUnitId) ?? "";
  }, [orgTree.data, presetUnitId]);

  const [text, setText] = useState("");
  const [focusToken, setFocusToken] = useState(0);
  const [attempts, setAttempts] = useState<WorkbenchAttempt[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);

  // A dispatch URL that no longer resolves (issue deleted, another device
  // removed it) falls back to a fresh composition instead of an empty thread.
  useEffect(() => {
    if (!selectedId || isLoading || selected) return;
    if (pathname !== workbenchPath) return;
    replace(workbenchPath);
  }, [selectedId, isLoading, selected, pathname, workbenchPath, replace]);

  const pending = attempts.some((attempt) => attempt.status === "pending");

  const submit = async (raw: string, unitId?: string, attemptId?: string) => {
    const trimmed = raw.trim();
    if (!trimmed || pending) return;
    // A new submission is its own thread; leaving a historical dispatch
    // selected would hide the pending turn until the server answered.
    if (selectedId) replace(workbenchPath);

    const id = attemptId ?? createSafeId();
    setAttempts((prev) => {
      const next: WorkbenchAttempt = {
        id,
        text: trimmed,
        status: "pending",
        candidates: [],
      };
      const existing = prev.findIndex((attempt) => attempt.id === id);
      if (existing === -1) return [...prev, next];
      return prev.map((attempt) => (attempt.id === id ? next : attempt));
    });

    try {
      const result = await dispatch.mutateAsync({
        text: trimmed,
        unitId,
        clientRequestId: id,
      });
      setAttempts((prev) => prev.filter((attempt) => attempt.id !== id));
      setText("");
      replace(p.workbenchDispatch(result.issue.id));
    } catch (err) {
      setAttempts((prev) =>
        prev.map((attempt) =>
          attempt.id === id
            ? {
                ...attempt,
                status: "failed",
                error: err,
                candidates: parseDispatchCandidates(err),
              }
            : attempt,
        ),
      );
    }
  };

  const selectDispatch = (item: DispatchListItem) => {
    push(p.workbenchDispatch(item.issue.id));
    setHistoryOpen(false);
  };

  const startNew = () => {
    push(workbenchPath);
    setHistoryOpen(false);
    setFocusToken((token) => token + 1);
  };

  const historyPanel = (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between px-3 py-2">
        <h2 className="text-caption font-medium text-muted-foreground">
          {t(($) => $.page.history)}
        </h2>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.page.new)}
          onClick={startNew}
        >
          <Plus />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        <HistoryList
          dispatches={dispatches}
          selectedId={selectedId}
          isLoading={isLoading}
          error={error}
          phaseFor={phaseFor}
          onSelect={selectDispatch}
        />
      </div>
    </div>
  );

  const conversation = selected ? (
    <DispatchConversation
      item={selected}
      phase={phaseFor(selected)}
      progress={progressFor(selected.issue.id)}
      onRetry={() =>
        void submit(selected.issue.description ?? selected.issue.title, selected.unit_id)
      }
    />
  ) : attempts.length > 0 ? (
    <div className="flex flex-col gap-8">
      {attempts.map((attempt) => (
        <DispatchAttemptConversation
          key={attempt.id}
          attempt={attempt}
          onRetry={() => void submit(attempt.text, undefined, attempt.id)}
          onPickCandidate={(candidate: DispatchCandidate) =>
            void submit(attempt.text, candidate.unit_id, attempt.id)
          }
        />
      ))}
    </div>
  ) : (
    <div className="flex flex-col items-center gap-5 py-10 text-center">
      <span className="flex size-12 items-center justify-center rounded-2xl bg-muted">
        <Sparkles className="size-6 text-muted-foreground" aria-hidden />
      </span>
      <div>
        <h1 className="text-title font-medium">{t(($) => $.page.empty_title)}</h1>
        <p className="mx-auto mt-1 max-w-md text-body text-muted-foreground">
          {t(($) => $.page.empty_description)}
        </p>
      </div>
      <div className="flex flex-col items-center gap-2">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.page.examples_label)}
        </p>
        <div className="flex max-w-xl flex-wrap justify-center gap-2">
          {[
            t(($) => $.page.examples.one),
            t(($) => $.page.examples.two),
            t(($) => $.page.examples.three),
          ].map((example) => (
            <Button
              key={example}
              variant="outline"
              size="sm"
              className="h-auto max-w-full py-1.5 whitespace-normal text-left"
              onClick={() => {
                setText(example);
                setFocusToken((token) => token + 1);
              }}
            >
              {example}
            </Button>
          ))}
        </div>
      </div>
    </div>
  );

  const conversationPane = (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6">
          {conversation}
        </div>
      </div>
      <div className="border-t bg-background/80 backdrop-blur">
        <div className="mx-auto w-full max-w-3xl px-4 py-3">
          <DispatchComposer
            value={text}
            onChange={setText}
            onSubmit={(value) =>
              void submit(value, presetUnitId ?? undefined)
            }
            pending={pending}
            unitPath={presetUnitId ? (presetUnitPath ?? "") : null}
            onClearUnit={
              presetUnitId
                ? () => replace(workbenchPath)
                : undefined
            }
            focusToken={focusToken}
          />
        </div>
      </div>
    </div>
  );

  const headerActions = isCompact ? (
    <>
      {selected ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => push(workbenchPath)}
          className="gap-1.5"
        >
          <ArrowLeft />
          {t(($) => $.page.back)}
        </Button>
      ) : null}
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t(($) => $.page.open_history)}
        onClick={() => setHistoryOpen(true)}
      >
        <Clock />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t(($) => $.page.new)}
        onClick={startNew}
      >
        <Plus />
      </Button>
    </>
  ) : undefined;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Sparkles}
        title={t(($) => $.page.title)}
        actions={headerActions}
      />
      <div className="flex min-h-0 flex-1">
        {!isCompact ? (
          <aside className="flex w-72 shrink-0 flex-col border-r">
            {historyPanel}
          </aside>
        ) : null}
        {conversationPane}
      </div>
      {isCompact ? (
        <Sheet open={historyOpen} onOpenChange={setHistoryOpen}>
          <SheetContent side="left" className="w-80 p-0">
            <SheetHeader className="sr-only">
              <SheetTitle>{t(($) => $.page.history)}</SheetTitle>
            </SheetHeader>
            {historyPanel}
          </SheetContent>
        </Sheet>
      ) : null}
    </div>
  );
}
