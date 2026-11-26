"use client";

import type { Agent, MemberWithUser } from "@multica/core/types";
import {
  SettingsCard,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation/app-link";
import { AccessPicker } from "./inspector/access-picker";

export function AgentAccessSettings({
  agent,
  members,
  currentUserId,
  sourceHref,
  onDirtyChange,
  onUpdate,
}: {
  agent: Agent;
  members: MemberWithUser[];
  currentUserId: string | null;
  sourceHref?: string | null;
  onDirtyChange?: (dirty: boolean) => void;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");

  return (
    <SettingsSection
      title={t(($) => $.access.section_title)}
      description={
        agent.is_primary === true && agent.owner_id === currentUserId
          ? t(($) => $.access.primary_twin_hint)
          : undefined
      }
    >
      <SettingsCard>
        <AccessPicker
          permissionMode={agent.permission_mode}
          invocationTargets={agent.invocation_targets}
          visibility={agent.visibility}
          members={members}
          ownerId={agent.owner_id}
          canEdit={
            !agent.source_agent_id &&
            currentUserId !== null &&
            agent.owner_id === currentUserId
          }
          readOnlyDescription={
            agent.source_agent_id
              ? t(($) => $.access.organization_managed_readonly)
              : undefined
          }
          hasComposioAllowlist={
            (agent.composio_toolkit_allowlist ?? []).length > 0
          }
          onDirtyChange={onDirtyChange}
          onChange={(next) => onUpdate(agent.id, next)}
        />
        {agent.source_agent_id && sourceHref && (
          <div className="flex justify-end px-4 py-3">
            <Button
              variant="outline"
              size="sm"
              render={<AppLink href={sourceHref} />}
              nativeButton={false}
            >
              {t(($) => $.detail.open_in_organization)}
            </Button>
          </div>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}
