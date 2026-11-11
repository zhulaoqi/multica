import { z } from "zod";

export const IntakeSubmitResponseSchema = z.object({
  issue_id: z.string(),
  enqueue_error: z.string().optional(),
});

export type IntakeSubmitResponse = z.infer<typeof IntakeSubmitResponseSchema>;

export const EMPTY_INTAKE_SUBMIT_RESPONSE: IntakeSubmitResponse = { issue_id: "" };

const intakeStatuses = ["等你补充", "已经停住", "待验收", "已完成", "正在做", "等待处理"] as const;

export const IntakeListItemSchema = z.object({
  issue_id: z.string(),
  identifier: z.string().optional(),
  body: z.string(),
  project_id: z.string().optional(),
  project_title: z.string().optional(),
  module_id: z.string().optional(),
  product: z.string().optional(),
  module: z.string().optional(),
  agent_id: z.string().optional(),
  agent_name: z.string().optional(),
  runtime_online: z.boolean().optional(),
  display_status: z.string().optional().transform((status) =>
    intakeStatuses.includes(status as (typeof intakeStatuses)[number]) ? status! : "等待处理",
  ),
  start_at: z.string().optional(),
  progress: z.object({
    done: z.string().optional(),
    remain: z.string().optional(),
  }).optional(),
  blocker: z.object({
    done: z.string().optional(),
    missing: z.string().optional(),
    consequence: z.string().optional(),
    state: z.string().optional(),
  }).optional(),
  result_summary: z.string().optional(),
  test_environment_url: z.string().optional(),
  run: z.object({
    status: z.string(),
    started_at: z.string().optional(),
    dispatched_at: z.string().optional(),
    agent_id: z.string().optional(),
    agent_name: z.string().optional(),
  }).optional(),
  can_continue: z.boolean().optional(),
  can_stop: z.boolean().optional(),
  can_accept: z.boolean().optional(),
  can_cancel: z.boolean().optional(),
  can_resume: z.boolean().optional(),
  can_match: z.boolean().optional(),
  unit_switches: z.number().optional(),
  unit_log: z.string().optional(),
});

export type IntakeListItem = z.infer<typeof IntakeListItemSchema>;

export const IntakeProjectItemSchema = IntakeListItemSchema.extend({
  project_id: z.string(),
  project_title: z.string(),
  mine: z.boolean(),
  can_decide: z.boolean(),
  elapsed: z.string().optional(),
});

export type IntakeProjectItem = z.infer<typeof IntakeProjectItemSchema>;

export const IntakeSessionSchema = z.object({
  id: z.string(),
  name: z.string(),
  can_maintain: z.boolean().optional(),
});

export type IntakeSession = z.infer<typeof IntakeSessionSchema>;

export const IntakeModuleSchema = z.object({
  id: z.string(),
  product: z.string(),
  module: z.string(),
  scope: z.string(),
  agent_id: z.string(),
  backup_agent_id: z.string().optional(),
});

export type IntakeModule = z.infer<typeof IntakeModuleSchema>;

export const IntakeAgentSchema = z.object({
  id: z.string(),
  name: z.string(),
  can: z.string().optional(),
  cannot: z.string().optional(),
  runtime_mode: z.string().optional(),
  online: z.boolean().optional(),
  active_count: z.number().optional(),
  placements: z.array(z.object({
    project: z.string(),
    product: z.string(),
    module: z.string(),
    role: z.string(),
  })).optional(),
});

export const IntakeMaintainProjectSchema = z.object({
  id: z.string(),
  title: z.string(),
});

export type IntakeMaintainProject = z.infer<typeof IntakeMaintainProjectSchema>;

export type IntakeAgent = z.infer<typeof IntakeAgentSchema>;
