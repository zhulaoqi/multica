/**
 * Text and eligibility rules for acting on a dispatch from its thread.
 *
 * Pure so the conversation, the review card and their tests agree on exactly
 * when a result can be accepted, and on the mention that brings the
 * department back after a rejection.
 */

import type { DispatchPhase } from "./use-workbench-conversation";

/** Only a delivered result waits on the requester; done/closed is settled. */
export function canReview(phase: DispatchPhase): boolean {
  return phase === "in_review";
}

/**
 * A markdown mention. Labels round-trip through the editor's tokenizer, which
 * escapes brackets, so a name like `QA [web]` must be escaped here too.
 */
export function subjectMention(
  assigneeType: string | null | undefined,
  assigneeId: string | null | undefined,
  label: string,
): string | null {
  if (!assigneeType || !assigneeId) return null;
  if (assigneeType !== "agent" && assigneeType !== "squad" && assigneeType !== "member") {
    return null;
  }
  const safeLabel = label.replace(/[[\]]/g, (char) => `\\${char}`);
  return `[@${safeLabel}](mention://${assigneeType}/${assigneeId})`;
}

/**
 * The comment that sends a rejected result back. The mention wakes the
 * department's owner; the reason follows so the next run knows what to fix.
 */
export function rejectionComment(mention: string | null, reason: string): string {
  const body = reason.trim();
  return mention ? `${mention}\n\n${body}` : body;
}

/**
 * A follow-up the requester adds to a live thread. Same shape as a rejection:
 * address the department so the message is an instruction, not a note.
 */
export function followUpComment(mention: string | null, text: string): string {
  const body = text.trim();
  return mention ? `${mention}\n\n${body}` : body;
}
