"use client";

import type { Agent, MemberRole, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

const ROLE_ORDER: Record<MemberRole, number> = {
  owner: 0,
  admin: 1,
  member: 2,
};

/**
 * A project's team is its people, not a department tree. Each member gets one
 * row with their organisation agents projected into this project; members who
 * have not bound an agent yet stay visible so the roster is the whole team.
 */
export function ProjectTeamRoster({
  members,
  agents,
  search,
  loading = false,
}: {
  members: readonly MemberWithUser[];
  agents: readonly Agent[];
  search: string;
  /** The roster query is still in flight; don't claim the team is empty. */
  loading?: boolean;
}) {
  const { t } = useT("agents");
  const { t: tMembers } = useT("members");
  const p = useWorkspacePaths();
  const myUserId = useAuthStore((s) => s.user?.id);

  const agentsByOwner = new Map<string, Agent[]>();
  for (const agent of agents) {
    if (!agent.owner_id || agent.archived_at) continue;
    const owned = agentsByOwner.get(agent.owner_id);
    if (owned) owned.push(agent);
    else agentsByOwner.set(agent.owner_id, [agent]);
  }

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

  if (visible.length === 0) {
    return (
      <div
        className="p-6 text-caption text-muted-foreground"
        data-testid="project-team-roster"
      >
        {loading
          ? null
          : query
            ? t(($) => $.page.no_member_match, { query: search.trim() })
            : t(($) => $.page.no_members)}
      </div>
    );
  }

  return (
    <div
      className="min-h-0 flex-1 overflow-y-auto"
      data-testid="project-team-roster"
    >
      <ul className="mx-auto flex w-full max-w-3xl flex-col gap-2 p-6">
        {visible.map((member) => {
          const owned = [...(agentsByOwner.get(member.user_id) ?? [])].sort(
            (a, b) =>
              Number(b.is_primary) - Number(a.is_primary) ||
              a.name.localeCompare(b.name),
          );
          return (
            <li
              key={member.user_id}
              className="flex items-center gap-3 rounded-lg border border-border px-4 py-3"
            >
              <ActorAvatar
                actorType="member"
                actorId={member.user_id}
                size="lg"
                enableHoverCard
              />
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <AppLink
                    href={p.memberDetail(member.user_id)}
                    className="truncate text-body font-medium hover:underline"
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
              </div>
              <div className="flex max-w-[55%] flex-wrap items-center justify-end gap-2">
                {owned.length > 0 ? (
                  owned.map((agent) => (
                    <AppLink
                      key={agent.id}
                      href={p.agentDetail(agent.id)}
                      className="flex items-center gap-2 rounded-md border border-border px-2 py-1 text-caption transition-colors hover:bg-accent"
                    >
                      <ActorAvatar
                        actorType="agent"
                        actorId={agent.id}
                        size="sm"
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
            </li>
          );
        })}
      </ul>
    </div>
  );
}
