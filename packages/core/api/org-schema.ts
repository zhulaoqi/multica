import { z } from "zod";

export const OrgUnitSchema = z.object({
  id: z.string(),
  parent_id: z.string().optional().default(""),
  name: z.string(),
  lead_user_id: z.string().optional().default(""),
  description: z.string().optional().default(""),
  position: z.number().optional().default(0),
});

export const OrgAgentSchema = z.object({
  id: z.string(),
  name: z.string(),
  unit_id: z.string().optional().default(""),
  can: z.string().optional().default(""),
  cannot: z.string().optional().default(""),
});

export const OrgTreeSchema = z.object({
  units: OrgUnitSchema.array().default([]),
  agents: OrgAgentSchema.array().default([]),
  can_manage: z.boolean().optional().default(false),
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

export type OrgUnit = z.infer<typeof OrgUnitSchema>;
export type OrgAgent = z.infer<typeof OrgAgentSchema>;
export type OrgTree = z.infer<typeof OrgTreeSchema>;
export type OrgImportRow = z.infer<typeof OrgImportRowSchema>;
export type OrgImportResult = z.infer<typeof OrgImportResultSchema>;

export const EMPTY_ORG_TREE: OrgTree = { units: [], agents: [], can_manage: false };
export const EMPTY_ORG_IMPORT_RESULT: OrgImportResult = {
  created_units: 0,
  existing_units: 0,
  agents_assigned: 0,
  unknown_agents: [],
  unknown_leads: [],
};
