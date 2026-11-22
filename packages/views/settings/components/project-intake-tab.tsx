"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  projectIntakeCandidatesOptions,
  projectIntakeSourcesOptions,
  useReplaceProjectIntakeSources,
} from "@multica/core/workspace";
import { IntakeSourceEditor } from "../../org/intake-sources";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

/**
 * Who may hand this project work. An empty list is not "nobody": the project
 * follows the intake rule of the department that owns it, which is what every
 * project did before project lists existed.
 */
export function ProjectIntakeTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { isManager } = useCurrentMember(wsId);
  const sources = useQuery(projectIntakeSourcesOptions(wsId));
  const candidates = useQuery(projectIntakeCandidatesOptions(wsId));
  const replace = useReplaceProjectIntakeSources(wsId);
  const [failed, setFailed] = useState(false);

  if (!wsId) return null;
  if (sources.isError) {
    return (
      <SettingsTab title={t(($) => $.page.project_intake.title)}>
        <p role="alert" className="text-caption text-muted-foreground">
          {t(($) => $.page.project_intake.unavailable)}
        </p>
      </SettingsTab>
    );
  }
  if (sources.isLoading || !sources.data) return null;

  const inheritedLabel = t(($) => $.page.project_intake.inherited, {
    path: sources.data.unit_path,
  });
  const members = (candidates.data?.members ?? []).map((member) => ({
    user_id: member.id,
    label: member.name,
  }));

  return (
    <SettingsTab
      title={t(($) => $.page.project_intake.title)}
      description={t(($) => $.page.project_intake.description)}
    >
      {failed ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.page.project_intake.save_failed)}
        </p>
      ) : null}
      <div className="rounded-lg border border-border p-4">
        <IntakeSourceEditor
          sources={sources.data.sources}
          unitChoices={candidates.data?.units ?? []}
          members={members}
          emptyLabel={inheritedLabel}
          disabled={!isManager}
          onSave={(next) => {
            setFailed(false);
            replace.mutate(next, { onError: () => setFailed(true) });
          }}
        />
      </div>
    </SettingsTab>
  );
}
