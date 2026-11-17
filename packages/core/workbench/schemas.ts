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
        unit_id: z.string(),
        unit_path: z.string().default(""),
      })
      .loose(),
  })
  .loose();

export const DispatchListItemSchema = z
  .object({
    issue: IssueSchema,
    unit_id: z.string(),
    unit_path: z.string().default(""),
    dispatched_at: z.string(),
    completed: z.boolean().default(false),
    notified_at: z.string().nullable().optional(),
    // Older servers omit it; the page treats unknown values as "none".
    leader_state: z.string().default("none"),
  })
  .loose();

export const DispatchListResponseSchema = z
  .object({
    dispatches: z.array(DispatchListItemSchema).default([]),
  })
  .loose();

export type DispatchCandidate = z.infer<typeof DispatchCandidateSchema>;
export type DispatchWorkResponse = z.infer<typeof DispatchWorkResponseSchema>;
export type DispatchListItem = z.infer<typeof DispatchListItemSchema>;
export type DispatchListResponse = z.infer<typeof DispatchListResponseSchema>;

export interface DispatchWorkInput {
  text: string;
  unit_id?: string;
  client_request_id: string;
}
