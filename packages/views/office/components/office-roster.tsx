"use client";

import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useT } from "../../i18n";
import type { AgentDeskStatus } from "../scene/characters";
import { AgentStatusBadge, initials } from "./agent-status";

export interface OfficeRosterMember {
  userId: string;
  name: string;
  isSelf: boolean;
}

export interface OfficeRosterAgent {
  id: string;
  name: string;
  status: AgentDeskStatus;
}

export interface OfficeRosterProps {
  members: OfficeRosterMember[];
  agents: OfficeRosterAgent[];
  joined: boolean;
  onAgentSelect: (agentId: string) => void;
}

export function OfficeRoster({ members, agents, joined, onAgentSelect }: OfficeRosterProps) {
  const { t } = useT("office");

  return (
    <aside className="flex w-full shrink-0 flex-col gap-6 border-t border-border/60 p-4 lg:w-72 lg:border-t-0 lg:border-l">
      <section className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between gap-2">
          <h2 className="text-caption font-medium uppercase tracking-wide text-muted-foreground">
            {t(($) => $.page.roster.title)}
          </h2>
          <span className="text-caption text-muted-foreground">
            {t(($) => $.page.roster.count, { count: members.length })}
          </span>
        </div>
        {members.length === 0 ? (
          <p className="text-body text-muted-foreground">
            {joined ? t(($) => $.page.roster.empty) : t(($) => $.page.connecting)}
          </p>
        ) : (
          <>
            <ul className="flex flex-col">
              {members.map((member) => (
                <li key={member.userId} className="flex items-center gap-2 py-1.5">
                  <ActorAvatar name={member.name} initials={initials(member.name)} size="sm" />
                  <span className="min-w-0 flex-1 truncate text-body">{member.name}</span>
                  {member.isSelf && (
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.page.roster.you)}
                    </span>
                  )}
                </li>
              ))}
            </ul>
            {members.every((member) => member.isSelf) && (
              <p className="text-body text-muted-foreground">
                {t(($) => $.page.roster.empty)}
              </p>
            )}
          </>
        )}
      </section>

      <section className="flex flex-col gap-2">
        <h2 className="text-caption font-medium uppercase tracking-wide text-muted-foreground">
          {t(($) => $.page.roster.agents)}
        </h2>
        {agents.length === 0 ? (
          <p className="text-body text-muted-foreground">
            {t(($) => $.page.roster.no_agents)}
          </p>
        ) : (
          <ul className="flex flex-col">
            {agents.map((agent) => (
              <li key={agent.id}>
                <button
                  type="button"
                  onClick={() => onAgentSelect(agent.id)}
                  className="flex w-full items-center gap-2 rounded-md px-1 py-1.5 text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <ActorAvatar
                    name={agent.name}
                    initials={initials(agent.name)}
                    isAgent
                    size="sm"
                  />
                  <span className="min-w-0 flex-1 truncate text-body">{agent.name}</span>
                  <AgentStatusBadge status={agent.status} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}
