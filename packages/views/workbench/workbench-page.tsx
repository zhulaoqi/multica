"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Building2, Clock, Plus, Sparkles } from "@multica/ui/icons";
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
import { findPrimaryTwin } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useFileUpload,
  type UploadResult,
} from "@multica/core/hooks/use-file-upload";
import { useCreateComment } from "@multica/core/issues/mutations";
import { useActorName, useOrganizationContext } from "@multica/core/workspace/hooks";
import { paths, useWorkspacePaths } from "@multica/core/paths";
import {
  parseDispatchCandidates,
  useDispatchWork,
  useMyDispatches,
  type DispatchCandidate,
  type DispatchListItem,
} from "@multica/core/workbench";
import {
  agentListOptions,
  orgTreeOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { createSafeId } from "@multica/core/utils";
import { CollectionPageHeader, CollectionPageState } from "../layout";
import { useT, useTimeAgo } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { unitPaths } from "../org/org-tree";
import { followUpComment, subjectMention } from "./dispatch-actions";
import { DepartmentQueue } from "./department-queue";
import { DispatchComposer, type ComposerMention } from "./dispatch-composer";
import {
  DispatchAttemptConversation,
  DispatchConversation,
  type WorkbenchAttempt,
} from "./dispatch-conversation";
import {
  useWorkbenchConversation,
  type DispatchPhase,
} from "./use-workbench-conversation";

/** Which slice of the history the requester is looking at. */
type DispatchFilter = "all" | "review" | "active" | "completed";

const FILTERS: DispatchFilter[] = ["all", "review", "active", "completed"];

function matchesFilter(phase: DispatchPhase, filter: DispatchFilter): boolean {
  switch (filter) {
    case "review":
      return phase === "in_review";
    case "active":
      return (
        phase === "queued" ||
        phase === "decomposing" ||
        phase === "progress" ||
        phase === "failed"
      );
    case "completed":
      return phase === "completed" || phase === "cancelled";
    default:
      return true;
  }
}

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
  filter,
  onFilterChange,
  onSelect,
}: {
  dispatches: DispatchListItem[];
  selectedId: string | null;
  isLoading: boolean;
  error: Error | null;
  phaseFor: (item: DispatchListItem) => DispatchPhase;
  filter: DispatchFilter;
  onFilterChange: (filter: DispatchFilter) => void;
  onSelect: (item: DispatchListItem) => void;
}) {
  const { t } = useT("workbench");
  const timeAgo = useTimeAgo();

  const counts: Record<DispatchFilter, number> = {
    all: dispatches.length,
    review: 0,
    active: 0,
    completed: 0,
  };
  for (const item of dispatches) {
    const phase = phaseFor(item);
    for (const key of ["review", "active", "completed"] as const) {
      if (matchesFilter(phase, key)) counts[key] += 1;
    }
  }
  const visible = dispatches.filter((item) => matchesFilter(phaseFor(item), filter));

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
    <div className="flex flex-col gap-1.5">
      <div role="group" aria-label={t(($) => $.page.filters.label)} className="flex flex-wrap gap-1">
        {FILTERS.map((key) => (
          <Button
            key={key}
            variant={filter === key ? "secondary" : "ghost"}
            size="sm"
            aria-pressed={filter === key}
            className="h-6 gap-1 rounded-full px-2 text-caption"
            onClick={() => onFilterChange(key)}
          >
            {t(($) => $.page.filters[key])}
            <span className="tabular-nums text-muted-foreground">{counts[key]}</span>
          </Button>
        ))}
      </div>
      {visible.length === 0 ? (
        <p className="px-2 py-3 text-caption text-muted-foreground">
          {t(($) => $.page.filters.empty)}
        </p>
      ) : null}
      <ul className="flex flex-col gap-0.5">
        {visible.map((item) => {
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
                  {item.personal
                    ? t(($) => $.page.personal_squad)
                    : item.unit_path || t(($) => $.page.unknown_department)}
                  {" · "}
                  {timeAgo(item.dispatched_at)}
                </span>
                {item.blocker?.missing ? (
                  <span className="min-w-0 truncate pl-3.5 text-caption text-warning">
                    {t(($) => $.page.blocked, { missing: item.blocker.missing })}
                  </span>
                ) : null}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
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
  const wsId = useWorkspaceId();
  const { organization, isProject } = useOrganizationContext(wsId);
  if (isProject && organization) {
    return <ProjectWorkbench organizationSlug={organization.slug} />;
  }
  return <DispatchWorkbench />;
}

/** Work is dispatched only in the organization; a project points there. */
function ProjectWorkbench({ organizationSlug }: { organizationSlug: string }) {
  const { t } = useT("workbench");
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader icon={Sparkles} title={t(($) => $.page.title)} />
      <CollectionPageState
        icon={Building2}
        title={t(($) => $.project.title)}
        description={t(($) => $.project.description)}
        actions={
          <Button
            render={<AppLink href={paths.workspace(organizationSlug).workbench()} />}
            nativeButton={false}
          >
            {t(($) => $.project.open_organization)}
          </Button>
        }
      />
    </div>
  );
}

function DispatchWorkbench() {
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
  // Sends to the requester's own personal squad instead of a department.
  const [personal, setPersonal] = useState(false);
  const [focusToken, setFocusToken] = useState(0);
  const [attempts, setAttempts] = useState<WorkbenchAttempt[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [filter, setFilter] = useState<DispatchFilter>("all");
  const [followUpPending, setFollowUpPending] = useState(false);
  const [followUpError, setFollowUpError] = useState<unknown>(null);
  const [attachments, setAttachments] = useState<UploadResult[]>([]);
  const [uploadFailed, setUploadFailed] = useState(false);
  const { upload, uploading } = useFileUpload(api);
  const followUp = useCreateComment(selected?.issue.id ?? "");
  const { getActorName } = useActorName();
  const userId = useAuthStore((state) => state.user?.id ?? "");
  const {
    data: agents = [],
    isSuccess: agentsLoaded,
  } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  // Dispatching needs an accountable twin: the request history is tied to the
  // member, and the department's follow-ups come back to this agent. Browsing
  // recent work stays available without one.
  const bindingRequired =
    agentsLoaded && !!userId && !findPrimaryTwin(agents.filter((agent) => !agent.archived_at), userId);

  const mentions = useMemo<ComposerMention[]>(
    () => [
      ...squads.map((squad) => ({
        type: "squad" as const,
        id: squad.id,
        label: squad.name,
        hint: t(($) => $.page.mention_squad),
      })),
      ...agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => ({
          type: "agent" as const,
          id: agent.id,
          label: agent.name,
          hint: t(($) => $.page.mention_agent),
        })),
    ],
    [agents, squads, t],
  );

  const attachFiles = async (files: FileList) => {
    setUploadFailed(false);
    for (const file of Array.from(files)) {
      try {
        const uploaded = await upload(file);
        if (uploaded) {
          setAttachments((prev) => [
            ...prev.filter((entry) => entry.id !== uploaded.id),
            uploaded,
          ]);
        }
      } catch {
        setUploadFailed(true);
      }
    }
  };

  const removeAttachment = (id: string) => {
    setAttachments((prev) => prev.filter((entry) => entry.id !== id));
  };
  const attachmentIds = attachments.map((entry) => entry.id);

  // A dispatch URL that no longer resolves (issue deleted, another device
  // removed it) falls back to a fresh composition instead of an empty thread.
  useEffect(() => {
    if (!selectedId || isLoading || selected) return;
    if (pathname !== workbenchPath) return;
    replace(workbenchPath);
  }, [selectedId, isLoading, selected, pathname, workbenchPath, replace]);

  const pending =
    attempts.some((attempt) => attempt.status === "pending") || followUpPending;

  /**
   * While a thread is open the composer adds to that conversation: the text
   * becomes a comment on the root issue that addresses the department, which
   * is how a requester asks a follow-up or changes scope mid-flight.
   */
  const sendFollowUp = async (raw: string) => {
    if (!selected) return;
    setFollowUpPending(true);
    setFollowUpError(null);
    try {
      const assigneeName =
        selected.issue.assignee_type && selected.issue.assignee_id
          ? getActorName(selected.issue.assignee_type, selected.issue.assignee_id)
          : selected.unit_path || t(($) => $.page.unknown_department);
      const mention = subjectMention(
        selected.issue.assignee_type,
        selected.issue.assignee_id,
        assigneeName,
      );
      await followUp.mutateAsync({
        content: followUpComment(mention, raw),
        attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
      });
      setText("");
      setAttachments([]);
    } catch (err) {
      setFollowUpError(err);
    } finally {
      setFollowUpPending(false);
    }
  };

  const submitDispatch = async (
    raw: string,
    unitId?: string,
    attemptId?: string,
    files: string[] = [],
    personalTarget = false,
  ) => {
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
        attachmentIds: files,
        personal: personalTarget,
      };
      const existing = prev.findIndex((attempt) => attempt.id === id);
      if (existing === -1) return [...prev, next];
      return prev.map((attempt) => (attempt.id === id ? next : attempt));
    });

    try {
      const result = await dispatch.mutateAsync({
        text: trimmed,
        unitId,
        personal: personalTarget,
        clientRequestId: id,
        attachmentIds: files.length > 0 ? files : undefined,
      });
      setAttempts((prev) => prev.filter((attempt) => attempt.id !== id));
      setText("");
      setAttachments([]);
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

  // Threads stay writable while unbound: a follow-up is part of reviewing
  // delivered work, not a new dispatch.
  const dispatchBlocked = bindingRequired && !selected;

  const submit = (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed || pending || dispatchBlocked) return;
    if (selected) {
      void sendFollowUp(trimmed);
      return;
    }
    // A preset department from the org page wins over the personal toggle.
    const targetPersonal = personal && !presetUnitId;
    void submitDispatch(
      trimmed,
      targetPersonal ? undefined : (presetUnitId ?? undefined),
      undefined,
      attachmentIds,
      targetPersonal,
    );
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
      <DepartmentQueue wsId={wsId} />
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        <HistoryList
          dispatches={dispatches}
          selectedId={selectedId}
          isLoading={isLoading}
          error={error}
          phaseFor={phaseFor}
          filter={filter}
          onFilterChange={setFilter}
          onSelect={selectDispatch}
        />
      </div>
    </div>
  );

  const conversation = selected ? (
    <DispatchConversation
      wsId={wsId}
      item={selected}
      phase={phaseFor(selected)}
      progress={progressFor(selected.issue.id)}
      onRetry={() =>
        void submitDispatch(
          selected.issue.description ?? selected.issue.title,
          selected.personal ? undefined : selected.unit_id,
          undefined,
          [],
          selected.personal,
        )
      }
    />
  ) : attempts.length > 0 ? (
    <div className="flex flex-col gap-8">
      {attempts.map((attempt) => (
        <DispatchAttemptConversation
          key={attempt.id}
          attempt={attempt}
          onRetry={() =>
            void submitDispatch(
              attempt.text,
              undefined,
              attempt.id,
              attempt.attachmentIds,
              attempt.personal,
            )
          }
          onPickCandidate={(candidate: DispatchCandidate) =>
            void submitDispatch(
              attempt.text,
              candidate.unit_id,
              attempt.id,
              attempt.attachmentIds,
            )
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
      {bindingRequired ? null : (
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
      )}
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
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-1.5 px-4 py-3">
          {followUpError ? (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.errors.followup_failed)}
            </p>
          ) : null}
          {uploadFailed ? (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.errors.upload_failed)}
            </p>
          ) : null}
          {!selected && !presetUnitId && !dispatchBlocked ? (
            <div
              role="group"
              aria-label={t(($) => $.page.target_label)}
              className="flex items-center gap-1"
            >
              <Button
                variant={personal ? "ghost" : "secondary"}
                size="sm"
                aria-pressed={!personal}
                className="h-6 rounded-full px-2 text-caption"
                onClick={() => setPersonal(false)}
              >
                {t(($) => $.page.target_department)}
              </Button>
              <Button
                variant={personal ? "secondary" : "ghost"}
                size="sm"
                aria-pressed={personal}
                className="h-6 rounded-full px-2 text-caption"
                onClick={() => setPersonal(true)}
              >
                {t(($) => $.page.target_personal)}
              </Button>
            </div>
          ) : null}
          {dispatchBlocked ? (
            <div className="flex flex-col gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3">
              <p className="text-body font-medium">{t(($) => $.page.binding_required_title)}</p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.page.binding_required_description)}
              </p>
              <Button size="sm" className="self-start" onClick={() => push(p.org())}>
                {t(($) => $.page.binding_required_action)}
              </Button>
            </div>
          ) : (
            <DispatchComposer
              value={text}
              onChange={setText}
              onSubmit={submit}
              pending={pending}
              unitPath={presetUnitId && !selected ? (presetUnitPath ?? "") : null}
              onClearUnit={
                presetUnitId && !selected
                  ? () => replace(workbenchPath)
                  : undefined
              }
              threadTitle={selected ? selected.issue.title : null}
              onExitThread={
                selected
                  ? () => {
                      setText("");
                      setFollowUpError(null);
                      setAttachments([]);
                      push(workbenchPath);
                    }
                  : undefined
              }
              attachments={attachments.map((entry) => ({
                id: entry.id,
                filename: entry.filename,
              }))}
              onAttachFiles={attachFiles}
              onRemoveAttachment={removeAttachment}
              uploading={uploading}
              mentions={mentions}
              focusToken={focusToken}
            />
          )}
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
