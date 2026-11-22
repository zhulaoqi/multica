import { z } from "zod";
import { IssueSchema } from "../api/schemas";

export const DispatchCandidateSchema = z
  .object({
    unit_id: z.string(),
    path: z.string().default(""),
    agent_count: z.number().default(0),
  })
  .loose();

export const DispatchWorkResponseSchema = z
  .object({
    issue: IssueSchema,
    dispatch: z
      .object({
        unit_id: z.string().default(""),
        unit_path: z.string().default(""),
        squad_id: z.string().default(""),
        personal: z.boolean().default(false),
        // The project the request was routed to, when the text named one or
        // the requester picked it. Older servers omit both.
        project_id: z.string().default(""),
        project_name: z.string().default(""),
      })
      .loose(),
  })
  .loose();

/** An open intake blocker: what is done, what is missing, what it costs. */
export const DispatchBlockerSchema = z
  .object({
    done: z.string().default(""),
    missing: z.string().default(""),
    consequence: z.string().default(""),
  })
  .loose();

export const DispatchListItemSchema = z
  .object({
    issue: IssueSchema,
    // A personal dispatch has no department; squad_id names its own squad.
    unit_id: z.string().default(""),
    unit_path: z.string().default(""),
    squad_id: z.string().default(""),
    personal: z.boolean().default(false),
    // The project the request named or selected; gone after a reassignment.
    project_id: z.string().default(""),
    project_name: z.string().default(""),
    dispatched_at: z.string(),
    completed: z.boolean().default(false),
    notified_at: z.string().nullable().optional(),
    // Older servers omit it; the page treats unknown values as "none".
    leader_state: z.string().default("none"),
    blocker: DispatchBlockerSchema.nullable().optional(),
  })
  .loose();

export const DispatchListResponseSchema = z
  .object({
    dispatches: z.array(DispatchListItemSchema).default([]),
  })
  .loose();

/** Which department queue an open dispatch is waiting in. */
export const DISPATCH_QUEUE_STAGES = ["pending", "in_progress", "review"] as const;
export const DispatchQueueStageSchema = z.enum(DISPATCH_QUEUE_STAGES);

export const DispatchQueueItemSchema = DispatchListItemSchema.extend({
  requester_id: z.string().default(""),
  requester_name: z.string().default(""),
  // Older servers do not have the queue endpoint at all, so a missing stage
  // falls back to the middle bucket instead of hiding the item.
  stage: DispatchQueueStageSchema.default("in_progress"),
});

export const DispatchQueueResponseSchema = z
  .object({
    dispatches: z.array(DispatchQueueItemSchema).default([]),
  })
  .loose();

export type DispatchCandidate = z.infer<typeof DispatchCandidateSchema>;
export type DispatchWorkResponse = z.infer<typeof DispatchWorkResponseSchema>;
export type DispatchListItem = z.infer<typeof DispatchListItemSchema>;
export type DispatchBlocker = z.infer<typeof DispatchBlockerSchema>;
export type DispatchQueueStage = z.infer<typeof DispatchQueueStageSchema>;
export type DispatchQueueItem = z.infer<typeof DispatchQueueItemSchema>;
export type DispatchListResponse = z.infer<typeof DispatchListResponseSchema>;

export interface DispatchWorkInput {
  text: string;
  unit_id?: string;
  /**
   * A project the requester may hand work. The work still lands on the
   * project's owning department; the project only gates who may ask.
   */
  project_id?: string;
  /** Send to the requester's own personal squad instead of a department. */
  personal?: boolean;
  client_request_id: string;
  /** Files uploaded before the dispatch; the server binds them to the root issue. */
  attachment_ids?: string[];
}
