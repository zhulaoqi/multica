"use client";

import { useMemo } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  DISPATCH_QUEUE_STAGES,
  useDepartmentDispatches,
  type DispatchQueueItem,
  type DispatchQueueStage,
} from "@multica/core/workbench";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

function stageDotClass(stage: DispatchQueueStage): string {
  switch (stage) {
    case "pending":
      return "bg-muted-foreground/50";
    case "review":
      return "bg-warning";
    default:
      return "bg-brand";
  }
}

/**
 * The department side of the workbench: open dispatches aimed at the units the
 * caller leads, grouped by the queue the department is expected to work them
 * from. An open blocker is shown inline because that is why the item is parked.
 * Hidden entirely when the caller leads no unit with open work.
 */
export function DepartmentQueue({ wsId }: { wsId: string }) {
  const { t } = useT("workbench");
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((state) => state.user?.id ?? "");
  const { dispatches, isLoading, error } = useDepartmentDispatches(wsId);

  // The requester accepts or rejects in their workbench thread; the issue page
  // has no review controls, so a lead who also requested the work goes there.
  const openItem = (item: DispatchQueueItem) => {
    const ownRequest = !!userId && item.requester_id === userId;
    push(ownRequest ? p.workbenchDispatch(item.issue.id) : p.issueDetail(item.issue.id));
  };

  const grouped = useMemo(() => {
    const buckets = new Map<DispatchQueueStage, DispatchQueueItem[]>(
      DISPATCH_QUEUE_STAGES.map((stage) => [stage, []]),
    );
    for (const item of dispatches) {
      buckets.get(item.stage)?.push(item);
    }
    return DISPATCH_QUEUE_STAGES.map((stage) => ({
      stage,
      items: buckets.get(stage) ?? [],
    })).filter((bucket) => bucket.items.length > 0);
  }, [dispatches]);

  if (isLoading && dispatches.length === 0) return null;
  if (error) {
    return (
      <p className="px-2 py-2 text-caption text-muted-foreground">
        {t(($) => $.queue.load_error)}
      </p>
    );
  }
  if (grouped.length === 0) return null;

  return (
    <section
      aria-label={t(($) => $.queue.title)}
      className="flex flex-col gap-2 border-t pb-2"
    >
      <h2 className="px-3 pt-2 text-caption font-medium text-muted-foreground">
        {t(($) => $.queue.title)}
      </h2>
      <div className="flex max-h-72 flex-col gap-2 overflow-y-auto px-2">
        {grouped.map(({ stage, items }) => (
          <div key={stage} className="flex flex-col gap-0.5">
            <p className="px-2 text-caption text-muted-foreground">
              {t(($) => $.queue.stages[stage])}
              <span className="ml-1 tabular-nums">{items.length}</span>
            </p>
            <ul className="flex flex-col gap-0.5">
              {items.map((item) => (
                <li key={item.issue.id}>
                  <button
                    type="button"
                    onClick={() => openItem(item)}
                    className="flex w-full flex-col gap-1 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-muted"
                  >
                    <span className="flex items-center gap-2">
                      <span
                        aria-hidden
                        className={cn(
                          "size-1.5 shrink-0 rounded-full",
                          stageDotClass(item.stage),
                        )}
                      />
                      <span className="min-w-0 flex-1 truncate text-body">
                        {item.issue.title}
                      </span>
                    </span>
                    <span className="min-w-0 truncate pl-3.5 text-caption text-muted-foreground">
                      {item.requester_name
                        ? t(($) => $.queue.requested_by, { name: item.requester_name })
                        : t(($) => $.queue.unknown_requester)}
                      {item.unit_path ? ` · ${item.unit_path}` : null}
                    </span>
                    {item.blocker?.missing ? (
                      <span className="min-w-0 truncate pl-3.5 text-caption text-warning">
                        {t(($) => $.queue.blocked, { missing: item.blocker.missing })}
                      </span>
                    ) : null}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}
