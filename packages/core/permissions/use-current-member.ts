"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import type { MemberRole, MemberWithUser } from "../types";
import { memberListOptions } from "../workspace/queries";

/**
 * Resolves the current user's membership in the given workspace. Single source
 * of truth for "what role am I" — replaces ad-hoc `members.find(...)` lookups
 * scattered across the views.
 *
 * The owning department's lead manages the project even when their stored role
 * is still `member`; `effectiveRole` folds that in so resource rules and
 * settings gates read one value. `role` stays the stored role, because owner-
 * only operations (deleting the project, assigning owners) must not be widened.
 *
 * `wsId` is explicit (not via `useWorkspaceId()` Context) so this hook stays
 * usable in components that may render before workspace context is wired,
 * matching the repo rule for workspace-aware hooks.
 */
export function useCurrentMember(wsId: string): {
  userId: string | null;
  role: MemberRole | null;
  /** Role plus department-lead authority, for project-level management. */
  effectiveRole: MemberRole | null;
  isOwner: boolean;
  /** Stored owner/admin only — used for project settings and admin surfaces. */
  isAdmin: boolean;
  /** True for the owning department's lead. */
  isOrgLead: boolean;
  /** Owner/admin or department lead — member management and project settings. */
  isManager: boolean;
  member: MemberWithUser | null;
  isLoading: boolean;
} {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members, isLoading } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const member = members?.find((m) => m.user_id === userId) ?? null;
  const role = member?.role ?? null;
  const isOrgLead = member?.is_org_lead ?? false;
  const isOwner = role === "owner";
  const isAdmin = isOwner || role === "admin";
  const isManager = isAdmin || isOrgLead;
  const effectiveRole: MemberRole | null =
    isOrgLead && role === "member" ? "admin" : role;
  return {
    userId,
    role,
    effectiveRole,
    isOwner,
    isAdmin,
    isOrgLead,
    isManager,
    member,
    isLoading,
  };
}
