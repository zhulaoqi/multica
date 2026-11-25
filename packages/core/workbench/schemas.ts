import { z } from "zod";
import { IssueSchema } from "../api/schemas";

const DispatchAttachmentSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    issue_id: z.string().nullable().default(null),
    comment_id: z.string().nullable().default(null),
    chat_session_id: z.string().nullable().default(null),
    chat_message_id: z.string().nullable().default(null),
    uploader_type: z.string().default(""),
    uploader_id: z.string().default(""),
    filename: z.string().default(""),
    url: z.string().default(""),
    download_url: z.string().default(""),
    markdown_url: z.string().default(""),
    content_type: z.string().default(""),
    size_bytes: z.number().default(0),
    created_at: z.string().default(""),
  })
  .loose();

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

/** Sub-issue completion of a dispatched root, read from its own workspace. */
export const DispatchProgressSchema = z
  .object({
    done: z.number().default(0),
    total: z.number().default(0),
  })
  .loose();

export const DispatchListItemSchema = z
  .object({
    issue: IssueSchema,
    request_attachments: z.array(DispatchAttachmentSchema).default([]),
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
    // Present for dispatches whose root lives in another workspace; older
    // servers omit it and the page falls back to the org-scoped child query.
    progress: DispatchProgressSchema.optional(),
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

/**
 * The requester's follow-up view of a dispatch delivered into another
 * workspace. It carries the tree summary, the agent's final answer and the
 * files it produced, but never internal comment bodies, descriptions, or tool
 * calls.
 */
export const DispatchTrailChildSchema = z
  .object({
    id: z.string(),
    title: z.string().default(""),
    status: z.string().default(""),
    status_category: z.string().default(""),
    status_label: z.string().default(""),
    assignee_type: z.string().default(""),
    assignee_id: z.string().default(""),
    assignee_name: z.string().default(""),
    created_at: z.string().default(""),
    started_at: z.string().nullable().optional(),
    completed_at: z.string().nullable().optional(),
  })
  .loose();

// Full attachment shape: the trail result feeds ReadonlyContent, which renders
// files from the same fields the timeline uses.
export const DispatchTrailAttachmentSchema = DispatchAttachmentSchema;

export const DispatchTrailResultSchema = z
  .object({
    actor_name: z.string().default(""),
    content: z.string().default(""),
    created_at: z.string().default(""),
    attachments: z.array(DispatchTrailAttachmentSchema).default([]),
  })
  .loose();

export const DispatchDeliverableSchema = z
  .object({
    key: z.string(),
    filename: z.string().default(""),
    content_type: z.string().default(""),
    size_bytes: z.number().default(0),
    version_count: z.number().default(1),
    download_url: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const DispatchTrailSchema = z
  .object({
    issue: IssueSchema,
    assignee_name: z.string().default(""),
    children: z.array(DispatchTrailChildSchema).default([]),
    progress: DispatchProgressSchema.default({ done: 0, total: 0 }),
    result: DispatchTrailResultSchema.nullable().optional(),
    deliverables: z.array(DispatchDeliverableSchema).default([]),
  })
  .loose();

/** Reply to a requester's follow-up: the updated issue and whether the run woke. */
export const DispatchCommentResponseSchema = z
  .object({
    issue: IssueSchema,
    delivered: z.boolean().default(false),
  })
  .loose();

/** Reply to the requester's verdict (accept / send back). */
export const DispatchStatusResponseSchema = z
  .object({
    issue: IssueSchema,
  })
  .loose();

export type DispatchCandidate = z.infer<typeof DispatchCandidateSchema>;
export type DispatchWorkResponse = z.infer<typeof DispatchWorkResponseSchema>;
export type DispatchListItem = z.infer<typeof DispatchListItemSchema>;
export type DispatchBlocker = z.infer<typeof DispatchBlockerSchema>;
export type DispatchProgress = z.infer<typeof DispatchProgressSchema>;
export type DispatchQueueStage = z.infer<typeof DispatchQueueStageSchema>;
export type DispatchQueueItem = z.infer<typeof DispatchQueueItemSchema>;
export type DispatchListResponse = z.infer<typeof DispatchListResponseSchema>;
export type DispatchTrailChild = z.infer<typeof DispatchTrailChildSchema>;
export type DispatchTrailAttachment = z.infer<typeof DispatchTrailAttachmentSchema>;
export type DispatchTrailResult = z.infer<typeof DispatchTrailResultSchema>;
export type DispatchDeliverable = z.infer<typeof DispatchDeliverableSchema>;
export type DispatchTrail = z.infer<typeof DispatchTrailSchema>;
export type DispatchCommentResponse = z.infer<typeof DispatchCommentResponseSchema>;
export type DispatchStatusResponse = z.infer<typeof DispatchStatusResponseSchema>;

export interface DispatchWorkInput {
  text: string;
  unit_id?: string;
  /**
   * A project the requester may hand work. The work is delivered into the
   * project's own workspace and run by one of its project agents.
   */
  project_id?: string;
  /** Send to the requester's own personal squad instead of a department. */
  personal?: boolean;
  client_request_id: string;
  /** Files uploaded before the dispatch; the server binds them to the root issue. */
  attachment_ids?: string[];
}
