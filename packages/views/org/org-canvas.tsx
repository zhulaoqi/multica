"use client";

import { Children, Fragment, useState, type ReactNode } from "react";
import { api, type OrgAgent, type OrgMember, type OrgSquad, type OrgUnit } from "@multica/core/api";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Bot, ChevronRight } from "@multica/ui/icons";
import { useT } from "../i18n";
import { IntroArtCard, IntroArtConnector } from "../layout/feature-intro";
import { agentAvailability, squadAvailability, type OrgNode } from "./org-tree";

export const NO_UNIT = "__none";

export type AvailabilityTone = "waiting" | "working" | "offline" | "attention" | "empty" | "unknown";

/** Small state chip: waiting for collaborative scheduling, busy, offline… */
export function StatusChip({ tone }: { tone: AvailabilityTone }) {
  const { t } = useT("org");
  const label = {
    waiting: t(($) => $.waiting_dispatch),
    working: t(($) => $.status_working),
    offline: t(($) => $.status_offline),
    attention: t(($) => $.status_attention),
    empty: t(($) => $.squad_empty),
    unknown: "",
  }[tone];
  if (!label) return null;
  const toneClass =
    tone === "working" ? "text-primary" : tone === "attention" ? "text-destructive" : "text-muted-foreground";
  return (
    <span className={`shrink-0 rounded-full border border-border/60 px-2 py-0.5 text-caption ${toneClass}`}>
      {label}
    </span>
  );
}

export function UnitPicker({
  value,
  choices,
  onChange,
  disabled,
}: {
  value: string;
  choices: Array<{ id: string; path: string }>;
  onChange: (unitId: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("org");
  const items = [
    { value: NO_UNIT, label: t(($) => $.unassigned) },
    ...choices.map((choice) => ({ value: choice.id, label: choice.path })),
  ];
  return (
    <Select
      items={items}
      value={value || NO_UNIT}
      onValueChange={(next) => onChange(!next || next === NO_UNIT ? "" : next)}
      disabled={disabled}
    >
      <SelectTrigger className="h-8 w-44 text-caption">
        <SelectValue>
          {() =>
            value
              ? (choices.find((choice) => choice.id === value)?.path ?? t(($) => $.move_to))
              : t(($) => $.unassigned)
          }
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NO_UNIT}>{t(($) => $.unassigned)}</SelectItem>
        {choices.map((choice) => (
          <SelectItem key={choice.id} value={choice.id}>
            {choice.path}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function MemberRow({
  member,
  canMove,
  choices,
  onMove,
}: {
  member: OrgMember;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  return (
    <div className="flex items-center gap-3 py-1.5">
      <ActorAvatar
        name={member.name}
        initials={member.name.charAt(0).toUpperCase()}
        avatarUrl={member.avatar_url ?? undefined}
        size="sm"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-body">{member.name}</div>
        <div className="truncate text-caption text-muted-foreground">{member.email}</div>
      </div>
      {canMove ? <UnitPicker value={member.unit_id} choices={choices} onChange={onMove} /> : null}
    </div>
  );
}

const WIRE = "border-dashed border-brand/50";

/** Vertical wire with an arrowhead, matching the intro art connectors. */
function Stem() {
  return <IntroArtConnector />;
}

/** A rail forking the parent card into a grid of child cards. */
function Branch({ children }: { children: ReactNode }) {
  const items = Children.toArray(children);
  return (
    <div className="relative w-full">
      {items.length > 1 ? (
        <span className={`absolute top-0 right-1/4 left-1/4 hidden border-t-[1.5px] sm:block ${WIRE}`} />
      ) : null}
      <div className="grid w-full gap-3 sm:grid-cols-2">
        {items.map((child, index) => (
          <div key={index} className="flex flex-col items-center">
            <span className={`h-3 w-0 shrink-0 border-l-[1.5px] ${WIRE}`} />
            <div className="w-full">{child}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="w-full">
      <h3 className="mb-1.5 text-caption font-medium text-muted-foreground">{label}</h3>
      <Branch>{children}</Branch>
    </section>
  );
}

function UnitHeaderCard({
  unit,
  path,
  isMine,
  memberCount,
  squadCount,
  agentCount,
  allMembers,
  actions,
}: {
  unit: OrgUnit;
  path: string;
  isMine: boolean;
  memberCount: number;
  squadCount: number;
  agentCount: number;
  allMembers: Array<{ user_id: string; label: string }>;
  actions: {
    run: (action: () => Promise<unknown>, fallback: string) => Promise<void>;
  };
}) {
  const { t } = useT("org");
  const { run } = actions;
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(unit.name);
  const [confirming, setConfirming] = useState(false);
  const leadLabel = unit.lead_user_id
    ? (allMembers.find((member) => member.user_id === unit.lead_user_id)?.label ?? unit.lead_user_id.slice(0, 8))
    : "";
  const leadItems = [
    { label: t(($) => $.no_lead), value: NO_UNIT },
    ...allMembers.map((member) => ({
      label: member.label,
      value: member.user_id,
    })),
  ];

  return (
    <IntroArtCard className="!p-4">
      <div className="flex flex-wrap items-center gap-2">
        {renaming ? (
          <>
            <Input value={draft} onChange={(event) => setDraft(event.target.value)} className="h-8 w-52" />
            <Button
              size="sm"
              onClick={() => {
                if (!draft.trim()) return;
                void run(
                  () => api.updateOrgUnit(unit.id, { name: draft.trim() }),
                  t(($) => $.action_failed),
                );
                setRenaming(false);
              }}
            >
              {t(($) => $.save)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setDraft(unit.name);
                setRenaming(false);
              }}
            >
              {t(($) => $.cancel)}
            </Button>
          </>
        ) : (
          <>
            <h2 className="text-heading">{unit.name}</h2>
            <span className="text-caption text-muted-foreground">{path}</span>
            {isMine ? (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-caption font-medium text-primary">
                {t(($) => $.my_department)}
              </span>
            ) : null}
            {unit.can_manage ? (
              <Button size="sm" variant="ghost" onClick={() => setRenaming(true)}>
                {t(($) => $.rename)}
              </Button>
            ) : null}
          </>
        )}
        {unit.can_manage ? (
          <>
            <span className="text-caption text-muted-foreground">{t(($) => $.lead)}</span>
            <Select
              items={leadItems}
              value={unit.lead_user_id || NO_UNIT}
              onValueChange={(next) =>
                void run(
                  () =>
                    api.updateOrgUnit(unit.id, {
                      lead_user_id: !next || next === NO_UNIT ? "" : next,
                    }),
                  t(($) => $.action_failed),
                )
              }
            >
              <SelectTrigger className="h-8 w-44 text-caption">
                <SelectValue>{() => leadLabel || t(($) => $.no_lead)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {leadItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {confirming ? (
              <>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    setConfirming(false);
                    void run(
                      () => api.deleteOrgUnit(unit.id),
                      t(($) => $.action_failed),
                    );
                  }}
                >
                  {t(($) => $.confirm_delete)}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                  {t(($) => $.cancel)}
                </Button>
              </>
            ) : (
              <Button size="sm" variant="ghost" onClick={() => setConfirming(true)}>
                {t(($) => $.delete)}
              </Button>
            )}
          </>
        ) : null}
      </div>
      <p className="mt-1 text-caption text-muted-foreground tabular-nums">
        {t(($) => $.members)} {memberCount} · {t(($) => $.squads)} {squadCount} · {t(($) => $.agents)} {agentCount}
      </p>
    </IntroArtCard>
  );
}

function ChildCard({ node, onSelect }: { node: OrgNode; onSelect: (unitId: string) => void }) {
  const count = node.members.length + node.agents.length + node.squads.length;
  return (
    <button
      type="button"
      onClick={() => onSelect(node.unit.id)}
      className="flex w-full items-center gap-2 rounded-lg bg-surface p-3 text-left ring-1 ring-surface-border transition-shadow hover:shadow-[var(--menu-shadow)] hover:ring-brand/50"
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate text-body font-medium">{node.unit.name}</span>
        <span className="block text-caption text-muted-foreground tabular-nums">{count}</span>
      </span>
      <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
    </button>
  );
}

export function SquadCard({
  squad,
  canMove,
  choices,
  onMove,
}: {
  squad: OrgSquad;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  const { t } = useT("org");
  return (
    <div className="flex w-full flex-col gap-2 rounded-lg bg-surface p-3 ring-1 ring-surface-border">
      <div className="flex items-center gap-2">
        <ActorAvatar name={squad.name} initials={squad.name.charAt(0).toUpperCase()} isSquad size="sm" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-body font-medium">{squad.name}</div>
          <div className="truncate text-caption text-muted-foreground">
            {t(($) => $.squad_agents, { count: squad.agent_count })}
          </div>
        </div>
        <StatusChip tone={squadAvailability(squad)} />
      </div>
      {canMove ? (
        <div className="flex justify-end">
          <UnitPicker value={squad.unit_id} choices={choices} onChange={onMove} />
        </div>
      ) : null}
    </div>
  );
}

export function AgentCard({
  agent,
  canMove,
  choices,
  onMove,
}: {
  agent: OrgAgent;
  canMove: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
}) {
  return (
    <div className="flex w-full flex-col gap-2 rounded-lg bg-surface p-3 ring-1 ring-surface-border">
      <div className="flex items-center gap-2">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <Bot className="size-4" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-body font-medium">{agent.name}</div>
          {agent.can ? <div className="truncate text-caption text-muted-foreground">{agent.can}</div> : null}
        </div>
        <StatusChip tone={agentAvailability(agent.status)} />
      </div>
      {canMove ? (
        <div className="flex justify-end">
          <UnitPicker value={agent.unit_id} choices={choices} onChange={onMove} />
        </div>
      ) : null}
    </div>
  );
}

/** The selected department drawn as a card tree: the unit card hands off to
 * child departments, squads, and agents through dashed wires, mirroring the
 * squad intro art so the organisation reads as a dispatch map. */
export function OrgCanvas({
  node,
  path,
  myUnitId,
  allMembers,
  unitChoices,
  actions,
  onSelect,
}: {
  node: OrgNode;
  path: string;
  myUnitId: string;
  allMembers: Array<{ user_id: string; label: string }>;
  unitChoices: Array<{ id: string; path: string; manage: boolean }>;
  actions: {
    run: (action: () => Promise<unknown>, fallback: string) => Promise<void>;
  };
  onSelect: (unitId: string) => void;
}) {
  const { t } = useT("org");
  const { unit, agents, members, squads, children } = node;
  const { run } = actions;
  const manageableChoices = unitChoices.filter((choice) => choice.manage);
  const [childName, setChildName] = useState("");

  const sections: Array<{ key: string; node: ReactNode }> = [];
  if (children.length > 0 || unit.can_manage) {
    sections.push({
      key: "children",
      node: (
        <Group label={t(($) => $.children)}>
          {children.map((child) => (
            <ChildCard key={child.unit.id} node={child} onSelect={onSelect} />
          ))}
          {unit.can_manage ? (
            <div className="flex w-full flex-col gap-2 rounded-lg border-[1.5px] border-dashed border-border p-3">
              <Input
                value={childName}
                onChange={(event) => setChildName(event.target.value)}
                placeholder={t(($) => $.unit_name_placeholder)}
                className="h-8"
              />
              <Button
                size="sm"
                disabled={!childName.trim()}
                onClick={() => {
                  const name = childName.trim();
                  if (!name) return;
                  void run(
                    () => api.createOrgUnit({ name, parent_id: unit.id }),
                    t(($) => $.action_failed),
                  );
                  setChildName("");
                }}
              >
                {t(($) => $.new_unit)}
              </Button>
            </div>
          ) : null}
        </Group>
      ),
    });
  }
  if (squads.length > 0) {
    sections.push({
      key: "squads",
      node: (
        <Group label={t(($) => $.squads)}>
          {squads.map((squad) => (
            <SquadCard
              key={squad.id}
              squad={squad}
              canMove={unit.can_manage}
              choices={manageableChoices}
              onMove={(unitId) =>
                void run(
                  () => api.setOrgSquadUnit(squad.id, unitId),
                  t(($) => $.action_failed),
                )
              }
            />
          ))}
        </Group>
      ),
    });
  }
  if (agents.length > 0) {
    sections.push({
      key: "agents",
      node: (
        <Group label={t(($) => $.agents)}>
          {agents.map((agent) => (
            <AgentCard
              key={agent.id}
              agent={agent}
              canMove={unit.can_manage}
              choices={manageableChoices}
              onMove={(unitId) =>
                void run(
                  () => api.setOrgAgentUnit(agent.id, unitId),
                  t(($) => $.action_failed),
                )
              }
            />
          ))}
        </Group>
      ),
    });
  }
  if (members.length > 0) {
    sections.push({
      key: "members",
      node: (
        <section className="w-full">
          <h3 className="mb-1.5 text-caption font-medium text-muted-foreground">{t(($) => $.members)}</h3>
          <IntroArtCard>
            {members.map((member) => (
              <MemberRow
                key={member.user_id}
                member={member}
                canMove={unit.can_manage}
                choices={manageableChoices}
                onMove={(unitId) =>
                  void run(
                    () => api.setOrgMemberUnit(member.user_id, unitId),
                    t(($) => $.action_failed),
                  )
                }
              />
            ))}
          </IntroArtCard>
        </section>
      ),
    });
  }

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col items-center gap-4">
      <UnitHeaderCard
        unit={unit}
        path={path}
        isMine={!!myUnitId && unit.id === myUnitId}
        memberCount={members.length}
        squadCount={squads.length}
        agentCount={agents.length}
        allMembers={allMembers}
        actions={actions}
      />
      {sections.map((section) => (
        <Fragment key={section.key}>
          <Stem />
          {section.node}
        </Fragment>
      ))}
    </div>
  );
}
