import { z } from "zod";

export const OrgIntakeSourceSchema = z.object({
  // "user" names one person; "org_unit" covers that department's whole subtree.
  type: z.enum(["user", "org_unit"]).catch("user"),
  id: z.string(),
});

export const OrgUnitSchema = z.object({
  id: z.string(),
  parent_id: z.string().optional().default(""),
  name: z.string(),
  lead_user_id: z.string().optional().default(""),
  // Whether the requester may edit this unit (the platform operator, or the
  // lead of the unit or one of its ancestors).
  can_manage: z.boolean().optional().default(false),
  description: z.string().optional().default(""),
  position: z.number().optional().default(0),
  // Who may hand this department work beyond its leads. Only units the
  // requester manages carry it; older servers omit it.
  intake_sources: OrgIntakeSourceSchema.array().optional().default([]),
});


/**
 * Who may hand one project work. An empty list is not "nobody": the project
 * then follows its owning department (`inherited`).
 */
export const ProjectIntakeSourcesSchema = z
  .object({
    sources: OrgIntakeSourceSchema.array().default([]),
    inherited: z.boolean().optional().default(true),
    unit_id: z.string().optional().default(""),
    unit_path: z.string().optional().default(""),
  })
  .loose();

/** Departments and people a project manager may put on its intake list. */
export const ProjectIntakeCandidatesSchema = z
  .object({
    units: z
      .object({ id: z.string(), path: z.string().optional().default("") })
      .loose()
      .array()
      .default([]),
    members: z
      .object({ id: z.string(), name: z.string().optional().default("") })
      .loose()
      .array()
      .default([]),
  })
  .loose();

/** Projects the caller may hand work, each with its owning department. */
export const RequestableProjectSchema = z
  .object({
    id: z.string(),
    name: z.string().default(""),
    slug: z.string().default(""),
    unit_id: z.string().default(""),
    unit_path: z.string().default(""),
  })
  .loose();

export const RequestableProjectsSchema = z
  .object({
    projects: RequestableProjectSchema.array().default([]),
  })
  .loose();

export const OrgMemberSchema = z.object({
  user_id: z.string(),
  unit_id: z.string().optional().default(""),
  name: z.string(),
  email: z.string().optional().default(""),
  avatar_url: z.string().nullable().optional().default(null),
  // Department colleagues appear in the tree whether or not they joined the
  // project. Older servers only returned members, so the default holds.
  in_project: z.boolean().optional().default(true),
});

export const OrgAgentSchema = z.object({
  id: z.string(),
  name: z.string(),
  unit_id: z.string().optional().default(""),
  // The member this agent belongs to when it is their registered digital
  // twin; empty for shared or unowned agents. Older servers omit it.
  owner_id: z.string().optional().default(""),
  // The owner's chosen primary twin. Older servers omit it, so the org view
  // falls back to their first owned agent.
  is_primary: z.boolean().optional().default(false),
  // Runtime backing the twin; the editor keeps it as configured.
  runtime_mode: z.string().optional().default(""),
  can: z.string().optional().default(""),
  cannot: z.string().optional().default(""),
  // Runtime availability (idle, working, blocked, error, offline). Older
  // servers omit it; the org view then treats the agent as unknown.
  status: z.string().optional().default(""),
});

export const OrgSquadSchema = z.object({
  id: z.string(),
  name: z.string(),
  unit_id: z.string().optional().default(""),
  leader_id: z.string().optional().default(""),
  agent_count: z.number().optional().default(0),
  working_agents: z.number().optional().default(0),
  // Dispatch guardrails enforced by the server when the leader creates a
  // child issue. Older servers omit them; the org view then hides the limits.
  wip_limit: z.number().optional().default(0),
  max_children: z.number().optional().default(0),
  // Work currently assigned to the squad, by status bucket.
  active_issues: z.number().optional().default(0),
  review_issues: z.number().optional().default(0),
  blocked_issues: z.number().optional().default(0),
});

export const OrgSquadSyncResultSchema = z.object({
  squad: OrgSquadSchema,
  added: z.number().optional().default(0),
  updated: z.number().optional().default(0),
  removed: z.number().optional().default(0),
  created: z.boolean().optional().default(false),
});

export const OrgTreeSchema = z.object({
  units: OrgUnitSchema.array().default([]),
  agents: OrgAgentSchema.array().default([]),
  members: OrgMemberSchema.array().default([]),
  squads: OrgSquadSchema.array().default([]),
  can_manage: z.boolean().optional().default(false),
  // The requester's own department, so plain members still land on their
  // organisation instead of an empty selection.
  my_unit_id: z.string().optional().default(""),
});

export const OrgUnitCreatedSchema = z.object({ id: z.string() });

export type OrgUnitCreated = z.infer<typeof OrgUnitCreatedSchema>;

export const OrgImportRowSchema = z.object({
  path: z.string(),
  lead_email: z.string().optional().default(""),
  agents: z.string().array().optional().default([]),
});

export const OrgImportResultSchema = z.object({
  created_units: z.number().default(0),
  existing_units: z.number().default(0),
  agents_assigned: z.number().default(0),
  unknown_agents: z.string().array().default([]),
  unknown_leads: z.string().array().default([]),
});

export const OrgSyncReportSchema = z.object({
  created_units: z.string().array().optional().default([]),
  updated_units: z.string().array().optional().default([]),
  leads_assigned: z.string().array().optional().default([]),
  members_added: z.string().array().optional().default([]),
  members_moved: z.string().array().optional().default([]),
  members_without_email: z.string().array().optional().default([]),
  detached_departments: z.string().array().optional().default([]),
  detached_users: z.string().array().optional().default([]),
  synced_at: z.string().optional().default(""),
});

export const OrgSyncStatusSchema = z.object({
  configured: z.boolean().optional().default(false),
  last_run: OrgSyncReportSchema.nullable().optional().default(null),
});

export type OrgSyncReport = z.infer<typeof OrgSyncReportSchema>;
export type OrgSyncStatus = z.infer<typeof OrgSyncStatusSchema>;

export type OrgUnit = z.infer<typeof OrgUnitSchema>;
export type OrgIntakeSource = z.infer<typeof OrgIntakeSourceSchema>;
export type OrgAgent = z.infer<typeof OrgAgentSchema>;
export type OrgMember = z.infer<typeof OrgMemberSchema>;
export type OrgSquad = z.infer<typeof OrgSquadSchema>;
export type OrgSquadSyncResult = z.infer<typeof OrgSquadSyncResultSchema>;
export type OrgTree = z.infer<typeof OrgTreeSchema>;
export type OrgImportRow = z.infer<typeof OrgImportRowSchema>;
export type OrgImportResult = z.infer<typeof OrgImportResultSchema>;
export type ProjectIntakeSources = z.infer<typeof ProjectIntakeSourcesSchema>;
export type ProjectIntakeCandidates = z.infer<typeof ProjectIntakeCandidatesSchema>;
export type RequestableProject = z.infer<typeof RequestableProjectSchema>;

export const EMPTY_ORG_TREE: OrgTree = {
  units: [],
  agents: [],
  members: [],
  squads: [],
  can_manage: false,
  my_unit_id: "",
};
export const EMPTY_ORG_SYNC_REPORT: OrgSyncReport = {
  created_units: [],
  updated_units: [],
  leads_assigned: [],
  members_added: [],
  members_moved: [],
  members_without_email: [],
  detached_departments: [],
  detached_users: [],
  synced_at: "",
};
export const EMPTY_ORG_SYNC_STATUS: OrgSyncStatus = { configured: false, last_run: null };
export const EMPTY_ORG_IMPORT_RESULT: OrgImportResult = {
  created_units: 0,
  existing_units: 0,
  agents_assigned: 0,
  unknown_agents: [],
  unknown_leads: [],
};
