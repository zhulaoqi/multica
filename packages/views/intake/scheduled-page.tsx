import type { IntakeListItem } from "@multica/core/api";

export function ScheduledList({ items, onCancel }: { items: IntakeListItem[]; onCancel: (issueId: string) => void }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="text-sm">到点执行</div>
      {items.length === 0 ? <div className="text-sm text-[var(--text-secondary)]">还没有要到点执行的需求</div> : null}
      {items.map((item) => (
        <div key={item.issue_id} className="rounded-md border px-3 py-2 text-sm">
          <div>{item.body}</div>
          <div>开始：{item.start_at}</div>
          <button type="button" onClick={() => onCancel(item.issue_id)} className="mt-2 h-8 rounded-md border px-3">取消</button>
        </div>
      ))}
    </div>
  );
}
