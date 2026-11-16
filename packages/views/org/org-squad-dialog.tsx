"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type OrgAgent, type OrgSquad, type OrgUnit } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { agentAvailability } from "./org-tree";

/**
 * Generates the department's squad from its roster, or re-syncs an existing
 * one. The leader is the only choice a human makes; members come from the
 * unit subtree and keep manual additions.
 */
export function GenerateSquadDialog({
  unit,
  agents,
  squad,
  open,
  onOpenChange,
}: {
  unit: OrgUnit;
  /** Agents placed in this unit or its sub-departments, plus the current
   * leader when it sits outside the subtree. */
  agents: OrgAgent[];
  squad: OrgSquad | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [name, setName] = useState(squad?.name ?? "");
  const [leaderId, setLeaderId] = useState(squad?.leader_id ?? "");
  const [wipLimit, setWipLimit] = useState(String(squad?.wip_limit || 3));
  const [maxChildren, setMaxChildren] = useState(String(squad?.max_children || 8));
  const [instructions, setInstructions] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const defaultName = t(($) => $.squad_default_name, { name: unit.name });
  const statusLabel = (status: string) =>
    ({
      waiting: t(($) => $.waiting_dispatch),
      working: t(($) => $.status_working),
      offline: t(($) => $.status_offline),
      attention: t(($) => $.status_attention),
      empty: t(($) => $.squad_empty),
      unknown: "",
    })[agentAvailability(status)];
  const leaderItems = agents.map((agent) => {
    const label = statusLabel(agent.status);
    return {
      value: agent.id,
      label: label ? `${agent.name} · ${label}` : agent.name,
    };
  });
  const parsedWip = Number.parseInt(wipLimit, 10);
  const parsedMax = Number.parseInt(maxChildren, 10);
  const limitsValid = parsedWip >= 1 && parsedWip <= 50 && parsedMax >= 1 && parsedMax <= 100;
  const ready = (squad !== null || leaderId !== "") && limitsValid;

  const submit = async () => {
    if (!ready) {
      setError(t(($) => $.squad_generate_invalid));
      return;
    }
    setSaving(true);
    setError("");
    try {
      await api.generateOrgUnitSquad(unit.id, {
        name: name.trim() || defaultName,
        leader_id: leaderId || undefined,
        wip_limit: parsedWip,
        max_children: parsedMax,
        instructions: instructions.trim() ? instructions.trim() : undefined,
      });
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
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
      <DialogContent className="max-w-md" data-testid="generate-squad-dialog">
        <DialogHeader>
          <DialogTitle>
            {squad ? t(($) => $.squad_sync_title) : t(($) => $.squad_generate_title, { name: unit.name })}
          </DialogTitle>
        </DialogHeader>
        <p className="text-caption text-muted-foreground">{t(($) => $.squad_generate_hint)}</p>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.squad_name_label)}</span>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={defaultName}
              disabled={squad !== null}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.squad_leader_label)}</span>
            <Select
              items={leaderItems}
              value={leaderId}
              onValueChange={(next) => setLeaderId(next ?? "")}
            >
              <SelectTrigger className="w-full">
                <SelectValue>{() => leaderItems.find((item) => item.value === leaderId)?.label ?? t(($) => $.squad_leader_placeholder)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {leaderItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <div className="flex gap-3">
            <label className="flex flex-1 flex-col gap-1">
              <span className="text-caption font-medium">{t(($) => $.squad_wip_limit)}</span>
              <Input
                type="number"
                min={1}
                max={50}
                value={wipLimit}
                onChange={(event) => setWipLimit(event.target.value)}
              />
            </label>
            <label className="flex flex-1 flex-col gap-1">
              <span className="text-caption font-medium">{t(($) => $.squad_max_children)}</span>
              <Input
                type="number"
                min={1}
                max={100}
                value={maxChildren}
                onChange={(event) => setMaxChildren(event.target.value)}
              />
            </label>
          </div>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.squad_instructions_label)}</span>
            <Textarea
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
              placeholder={t(($) => $.squad_instructions_placeholder)}
              className="min-h-16 resize-y"
            />
          </label>
        </div>
        {error ? <p className="text-body text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={saving}>
            {t(($) => $.cancel)}
          </Button>
          <Button size="sm" disabled={saving || !ready} onClick={() => void submit()}>
            {saving ? t(($) => $.squad_generating) : squad ? t(($) => $.squad_sync_action) : t(($) => $.squad_generate_action)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
