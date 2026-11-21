"use client";

import type { Agent, MemberRole, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Crown, FolderKanban } from "@multica/ui/icons";
import { ActorAvatar } from "../../common/actor-avatar";
import { IntroArtCard, IntroArtConnector } from "../../layout/feature-intro";
import { AppLink } from "../../navigation";
import { Branch } from "../../org/org-canvas";
import { useT } from "../../i18n";

const ROLE_ORDER: Record<MemberRole, number> = {
  owner: 0,
  admin: 1,
  member: 2,
};

/**
 * A project's team drawn as a tree of people: the project card names its
 * owners and hands off through the org chart's dashed wires to one card per
 * member, each listing the organisation agents projected into this project.
 * Members who have not bound an agent stay on the tree so it is the whole team.
 */
export function ProjectTeamRoster({
  members,
  agents,
  search,
}: {
  members: readonly MemberWithUser[];
  agents: readonly Agent[];
  search: string;
}) {
  const { t } = useT("agents");
  const { t: tLayout } = useT("layout");
  const projectName = useCurrentWorkspace()?.name ?? "";

  const agentsByOwner = new Map<string, Agent[]>();
  for (const agent of agents) {
    if (!agent.owner_id || agent.archived_at) continue;
    const owned = agentsByOwner.get(agent.owner_id);
    if (owned) owned.push(agent);
    else agentsByOwner.set(agent.owner_id, [agent]);
  }

  const owners = members.filter((member) => member.role === "owner");
  const query = search.trim().toLowerCase();
  const visible = members
    .filter(
      (member) =>
        !query ||
        member.name.toLowerCase().includes(query) ||
        member.email.toLowerCase().includes(query),
    )
    .sort(
      (a, b) =>
        ROLE_ORDER[a.role] - ROLE_ORDER[b.role] || a.name.localeCompare(b.name),
    );

  return (
    <div
      className="min-h-0 flex-1 overflow-y-auto"
      data-testid="project-team-roster"
    >
      <div className="mx-auto flex w-full max-w-3xl flex-col items-center p-6">
        <IntroArtCard className="!p-3 sm:w-4/5">
          <div className="flex items-center gap-2">
            <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
              <FolderKanban className="size-4" aria-hidden />
            </span>
            <span className="min-w-0 flex-1 truncate text-body font-medium">
              {projectName}
            </span>
            <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
              {tLayout(($) => $.panels.team.members, { count: members.length })}
            </span>
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-1.5 pl-9">
            <span className="text-muted-foreground">
              {t(($) => $.team_tree.department_lead)}
            </span>
            {owners.length > 0 ? (
              owners.map((owner) => (
                <span
                  key={owner.user_id}
                  className="flex min-w-0 items-center gap-1 rounded-full bg-muted py-0.5 pr-2 pl-0.5"
                >
                  <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />
                  <span className="truncate font-medium">{owner.name}</span>
                  <Crown className="size-3 shrink-0 text-warning" aria-hidden />
                </span>
              ))
            ) : (
              <span className="text-muted-foreground">
                {t(($) => $.team_tree.no_lead)}
              </span>
            )}
          </div>
        </IntroArtCard>
        {visible.length > 0 ? (
          <>
            <IntroArtConnector />
            <Branch>
              {visible.map((member) => (
                <MemberCard
                  key={member.user_id}
                  member={member}
                  agents={agentsByOwner.get(member.user_id) ?? []}
                />
              ))}
            </Branch>
          </>
        ) : (
          <p className="mt-6 text-caption text-muted-foreground">
            {query
              ? t(($) => $.page.no_member_match, { query: search.trim() })
              : t(($) => $.page.no_members)}
          </p>
        )}
      </div>
    </div>
  );
}

function MemberCard({
  member,
  agents,
}: {
  member: MemberWithUser;
  agents: readonly Agent[];
}) {
  const { t } = useT("agents");
  const { t: tMembers } = useT("members");
  const p = useWorkspacePaths();
  const myUserId = useAuthStore((s) => s.user?.id);
  const owned = [...agents].sort(
    (a, b) =>
      Number(b.is_primary) - Number(a.is_primary) || a.name.localeCompare(b.name),
  );

  return (
    <IntroArtCard className="flex h-full flex-col gap-2 !p-3">
      <div className="flex items-center gap-2">
        <ActorAvatar
          actorType="member"
          actorId={member.user_id}
          size="sm"
          enableHoverCard
        />
        <AppLink
          href={p.memberDetail(member.user_id)}
          className="min-w-0 flex-1 truncate text-body font-medium hover:underline"
        >
          {member.name}
        </AppLink>
        {member.user_id === myUserId && (
          <Badge variant="secondary" className="shrink-0">
            {t(($) => $.row.you)}
          </Badge>
        )}
        <Badge variant="secondary" className="shrink-0">
          {member.role === "owner"
            ? tMembers(($) => $.role.owner)
            : member.role === "admin"
              ? tMembers(($) => $.role.admin)
              : tMembers(($) => $.role.member)}
        </Badge>
      </div>
      <div className="truncate text-caption text-muted-foreground">
        {member.email}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {owned.length > 0 ? (
          owned.map((agent) => (
            <AppLink
              key={agent.id}
              href={p.agentDetail(agent.id)}
              className="flex min-w-0 items-center gap-1.5 rounded-md border border-border px-2 py-1 text-caption transition-colors hover:bg-accent"
            >
              <ActorAvatar
                actorType="agent"
                actorId={agent.id}
                size="xs"
                showStatusDot
                profileLink={false}
              />
              <span className="max-w-40 truncate">{agent.name}</span>
            </AppLink>
          ))
        ) : (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.page.no_agent)}
          </span>
        )}
      </div>
    </IntroArtCard>
  );
}
