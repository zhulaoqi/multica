"use client";

import { Children, Fragment, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { api, type OrgAgent, type OrgMember, type OrgSquad, type OrgUnit } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@multica/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Bot, ChevronDown, ChevronRight } from "@multica/ui/icons";
import { useT } from "../i18n";
import { IntroArtCard, IntroArtConnector } from "../layout/feature-intro";
import { DispatchDialog } from "../squads/components/dispatch-dialog";
import { agentAvailability, memberTwin, squadAvailability, type OrgNode } from "./org-tree";
import { GenerateSquadDialog } from "./org-squad-dialog";
import { TwinDialog } from "./org-twin";

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
  const [open, setOpen] = useState(false);
  const label = value
    ? (choices.find((choice) => choice.id === value)?.path ?? t(($) => $.move_to))
    : t(($) => $.unassigned);
  const pick = (unitId: string) => {
    onChange(unitId);
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        render={
          <button
            type="button"
            role="combobox"
            aria-expanded={open}
            disabled={disabled}
            className="flex h-8 w-44 items-center gap-1.5 rounded-lg border border-input bg-transparent px-2.5 text-left text-caption outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30"
          />
        }
      >
        <span className="min-w-0 flex-1 truncate">{label}</span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <Command
          filter={(itemValue, search) =>
            itemValue.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) ? 1 : 0
          }
        >
          <CommandInput placeholder={t(($) => $.picker_search)} />
          <CommandList>
            <CommandEmpty>{t(($) => $.picker_empty)}</CommandEmpty>
            <CommandItem value={t(($) => $.unassigned)} onSelect={() => pick("")}>
              {t(($) => $.unassigned)}
            </CommandItem>
            {choices.map((choice) => (
              <CommandItem
                key={choice.id}
                value={`${choice.path} ${choice.id}`}
                onSelect={() => pick(choice.id)}
              >
                <span className="truncate">{choice.path}</span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function MemberRow({
  member,
  canMove,
  canConfigureTwin = false,
  choices,
  onMove,
  twin = null,
  placeUnitId = "",
  canAddToProject = false,
  onAddToProject,
}: {
  member: OrgMember;
  canMove: boolean;
  /** The caller leads this member's department (or the workspace), so they
   * may configure the member's twin as well as their own. */
  canConfigureTwin?: boolean;
  choices: Array<{ id: string; path: string }>;
  onMove: (unitId: string) => void;
  twin?: OrgAgent | null;
  placeUnitId?: string;
  /** The caller manages project members, so a department colleague outside
   * the project can be pulled in from here. */
  canAddToProject?: boolean;
  onAddToProject?: (userId: string) => void | Promise<void>;
}) {
  const { t } = useT("org");
  const myUserId = useAuthStore((state) => state.user?.id ?? "");
  const isSelf = member.user_id === myUserId;
  const canEditTwin = isSelf || canConfigureTwin;
  const [dialogOpen, setDialogOpen] = useState(false);
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
        {!member.in_project ? (
          <div className="mt-1 flex items-center gap-2">
            <span className="text-caption text-muted-foreground">
              {t(($) => $.not_in_project)}
            </span>
            {canAddToProject && onAddToProject ? (
              <Button
                size="sm"
                variant="ghost"
                className="h-7 px-2 text-caption"
                onClick={() => void onAddToProject(member.user_id)}
              >
                {t(($) => $.add_to_project)}
              </Button>
            ) : null}
          </div>
        ) : null}
        {twin ? (
          <div className="mt-1 flex items-center gap-2 rounded-md bg-muted/40 px-2 py-1">
            <Bot className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
            <span className="shrink-0 text-caption font-medium text-muted-foreground">{t(($) => $.twin)}</span>
            <span className="shrink-0 truncate text-caption">{twin.name}</span>
            <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{twin.can}</span>
            <StatusChip tone={agentAvailability(twin.status)} />
            {canEditTwin ? (
              <button
                type="button"
                className="shrink-0 text-caption text-primary hover:underline"
                onClick={() => setDialogOpen(true)}
              >
                {t(($) => $.edit_twin)}
              </button>
            ) : null}
          </div>
        ) : canEditTwin ? (
          <div className="mt-1">
            <Button size="sm" variant="ghost" className="h-7 px-2 text-caption" onClick={() => setDialogOpen(true)}>
              {isSelf ? t(($) => $.twin_register) : t(($) => $.twin_register_for, { name: member.name })}
            </Button>
          </div>
        ) : null}
      </div>
      {canMove ? <UnitPicker value={member.unit_id} choices={choices} onChange={onMove} /> : null}
      {dialogOpen ? (
        <TwinDialog
          twin={twin}
          placeUnitId={placeUnitId}
          ownerId={isSelf ? undefined : member.user_id}
          ownerName={isSelf ? "" : member.name}
          open
          onOpenChange={setDialogOpen}
        />
      ) : null}
    </div>
  );
}

/** CreateUnitDialog is the compact replacement for the create-child card that
 * used to sit inside the card grid: a small header button opens it. */
export function CreateUnitDialog({
  unitId,
  open,
  onOpenChange,
}: {
  unitId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const submit = async () => {
    setSaving(true);
    setError("");
    try {
      await api.createOrgUnit({ name: name.trim(), parent_id: unitId });
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
      setName("");
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.action_failed));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!saving) onOpenChange(next);
      }}
    >
      <DialogContent className="max-w-sm" data-testid="create-unit-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.create_child)}</DialogTitle>
        </DialogHeader>
        <Input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder={t(($) => $.unit_name_placeholder)}
        />
        {error ? <p className="text-body text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={saving}>
            {t(($) => $.cancel)}
          </Button>
          <Button size="sm" disabled={saving || !name.trim()} onClick={() => void submit()}>
            {t(($) => $.new_unit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const WIRE = "border-dashed border-brand/50";

/** Vertical wire with an arrowhead, matching the intro art connectors. */
function Stem() {
  return <IntroArtConnector />;
}

/** A rail forking the parent card into a grid of child cards. */
export function Branch({ children }: { children: ReactNode }) {
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
  squad,
  squadAgents,
  actions,
}: {
  unit: OrgUnit;
  path: string;
  isMine: boolean;
  memberCount: number;
  squadCount: number;
  agentCount: number;
  allMembers: Array<{ user_id: string; label: string }>;
  /** The squad generated for this department, if any. */
  squad: OrgSquad | null;
  /** Agents this department and its sub-departments cover, for leader choice. */
  squadAgents: OrgAgent[];
  actions: {
    run: (action: () => Promise<unknown>, fallback: string) => Promise<void>;
  };
}) {
  const { t } = useT("org");
  const { run } = actions;
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(unit.name);
  const [confirming, setConfirming] = useState(false);
  const [creating, setCreating] = useState(false);
  const [generating, setGenerating] = useState(false);
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
            {unit.can_manage ? (
              <Button size="sm" variant="ghost" onClick={() => setCreating(true)}>
                {t(($) => $.create_child)}
              </Button>
            ) : null}
            {unit.can_manage ? (
              <Button
                size="sm"
                variant="ghost"
                disabled={squadAgents.length === 0}
                title={squadAgents.length === 0 ? t(($) => $.squad_generate_no_agents) : undefined}
                onClick={() => setGenerating(true)}
              >
                {squad ? t(($) => $.squad_sync_action) : t(($) => $.squad_generate_action)}
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
      {creating ? <CreateUnitDialog unitId={unit.id} open onOpenChange={setCreating} /> : null}
      {generating ? (
        <GenerateSquadDialog
          unit={unit}
          agents={squadAgents}
          squad={squad}
          open
          onOpenChange={setGenerating}
        />
      ) : null}
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
  const [dispatching, setDispatching] = useState(false);
  const issueCounts = t(($) => $.squad_issue_counts, {
    active: squad.active_issues,
    review: squad.review_issues,
    blocked: squad.blocked_issues,
  });
  return (
    <div className="flex w-full flex-col gap-2 rounded-lg bg-surface p-3 ring-1 ring-surface-border">
      <div className="flex items-center gap-2">
        <ActorAvatar name={squad.name} initials={squad.name.charAt(0).toUpperCase()} isSquad size="sm" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-body font-medium">{squad.name}</div>
          <div className="truncate text-caption text-muted-foreground">
            {t(($) => $.squad_agents, { count: squad.agent_count })} · {issueCounts}
          </div>
        </div>
        <StatusChip tone={squadAvailability(squad)} />
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-caption text-muted-foreground tabular-nums">
          {t(($) => $.squad_limits, { wip: squad.wip_limit, max: squad.max_children })}
        </span>
        <div className="flex items-center gap-1">
          {canMove ? <UnitPicker value={squad.unit_id} choices={choices} onChange={onMove} /> : null}
          <Button size="sm" variant="ghost" className="h-7 px-2 text-caption" onClick={() => setDispatching(true)}>
            {t(($) => $.dispatch_work)}
          </Button>
        </div>
      </div>
      {dispatching ? (
        <DispatchDialog
          target={{ type: "squad", id: squad.id, name: squad.name }}
          open
          onOpenChange={setDispatching}
        />
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
  const { t } = useT("org");
  const [dispatching, setDispatching] = useState(false);
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
      <div className="flex items-center justify-end gap-1">
        {canMove ? <UnitPicker value={agent.unit_id} choices={choices} onChange={onMove} /> : null}
        <Button size="sm" variant="ghost" className="h-7 px-2 text-caption" onClick={() => setDispatching(true)}>
          {t(($) => $.dispatch_work)}
        </Button>
      </div>
      {dispatching ? (
        <DispatchDialog
          target={{ type: "agent", id: agent.id, name: agent.name }}
          open
          onOpenChange={setDispatching}
        />
      ) : null}
    </div>
  );
}

function collectSubtreeAgents(node: OrgNode): OrgAgent[] {
  return [...node.agents, ...node.children.flatMap(collectSubtreeAgents)];
}

/** The selected department drawn as a card tree: the unit card hands off to
 * child departments, squads, and agents through dashed wires, mirroring the
 * squad intro art so the organisation reads as a dispatch map. */
export function OrgCanvas({
  node,
  path,
  myUnitId,
  allMembers,
  allAgents,
  unitChoices,
  actions,
  onSelect,
  canAddToProject,
  onAddToProject,
}: {
  node: OrgNode;
  path: string;
  myUnitId: string;
  allMembers: Array<{ user_id: string; label: string }>;
  /** Every agent in the workspace, so a squad leader outside the subtree
   * still appears in the leader picker. */
  allAgents: OrgAgent[];
  unitChoices: Array<{ id: string; path: string; manage: boolean }>;
  actions: {
    run: (action: () => Promise<unknown>, fallback: string) => Promise<void>;
  };
  onSelect: (unitId: string) => void;
  canAddToProject?: boolean;
  onAddToProject?: (userId: string) => void | Promise<void>;
}) {
  const { t } = useT("org");
  const { unit, agents, members, squads, children } = node;
  const { run } = actions;
  const manageableChoices = unitChoices.filter((choice) => choice.manage);
  const unitSquad = squads.find((squad) => squad.unit_id === unit.id) ?? null;
  const subtreeAgents = collectSubtreeAgents(node);
  const leaderAgent =
    unitSquad && !subtreeAgents.some((agent) => agent.id === unitSquad.leader_id)
      ? (allAgents.find((agent) => agent.id === unitSquad.leader_id) ?? null)
      : null;
  const squadAgents = leaderAgent ? [...subtreeAgents, leaderAgent] : subtreeAgents;

  // An agent owned by a member shown here is that member's twin; it renders
  // on the member row instead of duplicating in the shared agents grid.
  const twins = new Map<string, OrgAgent>();
  for (const member of members) {
    const twin = memberTwin(agents, member.user_id);
    if (twin) twins.set(member.user_id, twin);
  }
  const twinIds = new Set([...twins.values()].map((twin) => twin.id));
  const sharedAgents = agents.filter((agent) => !twinIds.has(agent.id));

  const sections: Array<{ key: string; node: ReactNode }> = [];
  if (children.length > 0) {
    sections.push({
      key: "children",
      node: (
        <Group label={t(($) => $.children)}>
          {children.map((child) => (
            <ChildCard key={child.unit.id} node={child} onSelect={onSelect} />
          ))}
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
  if (sharedAgents.length > 0) {
    sections.push({
      key: "agents",
      node: (
        <Group label={t(($) => $.agents)}>
          {sharedAgents.map((agent) => (
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
                canConfigureTwin={unit.can_manage}
                choices={manageableChoices}
                twin={twins.get(member.user_id) ?? null}
                placeUnitId={member.unit_id}
                canAddToProject={canAddToProject}
                onAddToProject={onAddToProject}
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
        squad={unitSquad}
        squadAgents={squadAgents}
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
