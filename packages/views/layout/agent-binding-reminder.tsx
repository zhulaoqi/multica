"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core";
import { findPrimaryTwin } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

// Desktop remounts the workspace layout per tab, so the reminder is tracked
// per app session rather than per mount.
const remindedWorkspaces = new Set<string>();

/**
 * Reminds a member without a primary twin to bind one, once per workspace per
 * session. Unbound members can browse and review but cannot dispatch work or
 * be assigned issues, so the reminder points them at the org page where they
 * bind their agent. Mount inside a resolved workspace (DashboardLayout on web,
 * WorkspaceRouteLayout on desktop).
 */
export function AgentBindingReminder() {
  const { t } = useT("workbench");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((state) => state.user?.id ?? "");
  const { data: agents, isSuccess } = useQuery(agentListOptions(wsId));

  const unbound =
    isSuccess &&
    !!userId &&
    !findPrimaryTwin(
      (agents ?? []).filter((agent) => !agent.archived_at),
      userId,
    );

  useEffect(() => {
    if (!unbound || remindedWorkspaces.has(wsId)) return;
    remindedWorkspaces.add(wsId);
    toast.warning(t(($) => $.page.binding_required_title), {
      id: `agent-binding-reminder-${wsId}`,
      description: t(($) => $.page.binding_required_description),
      duration: 10_000,
      action: {
        label: t(($) => $.page.binding_required_action),
        onClick: () => push(p.org()),
      },
    });
  }, [unbound, wsId, t, push, p]);

  return null;
}
