"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type OrgAgent } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { AppLink } from "../navigation";

/** TwinDialog edits the digital twin (the member's own agent) or registers
 * the first one. A new twin is placed into the member's department right
 * after registration so it shows up in the org tree immediately. ownerId is
 * the member the twin belongs to — a department lead may configure a direct
 * report's twin, so it can differ from the signed-in user. */
export function TwinDialog({
  twin,
  placeUnitId,
  ownerId,
  ownerName = "",
  open,
  onOpenChange,
}: {
  twin: OrgAgent | null;
  placeUnitId: string;
  ownerId?: string;
  ownerName?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const queryClient = useQueryClient();
  const [name, setName] = useState(twin?.name ?? "");
  const [can, setCan] = useState(twin?.can ?? "");
  const [cannot, setCannot] = useState(twin?.cannot ?? "");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      if (twin) {
        await api.patchIntakeAgent(twin.id, {
          name: name.trim(),
          can: can.trim(),
          cannot: cannot.trim(),
          runtime_mode: twin.runtime_mode === "cloud" ? "cloud" : "local",
        });
      } else {
        const created = await api.registerIntakeAgent({
          name: name.trim(),
          can: can.trim(),
          cannot: cannot.trim(),
          runtime_mode: "local",
          ...(ownerId ? { owner_id: ownerId } : {}),
        });
        if (placeUnitId) {
          await api.setOrgAgentUnit(created.id, placeUnitId);
        }
      }
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.org(wsId) });
      await queryClient.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.twin_failed));
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
      <DialogContent className="max-w-md" data-testid="twin-dialog">
        <DialogHeader>
          <DialogTitle>
            {ownerName ? t(($) => $.twin_title_for, { name: ownerName }) : t(($) => $.twin_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.twin_desc)}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.twin_name)}</span>
            <Input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.twin_can)}</span>
            <Textarea value={can} onChange={(event) => setCan(event.target.value)} className="min-h-20 resize-y" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium">{t(($) => $.twin_cannot)}</span>
            <Textarea
              value={cannot}
              onChange={(event) => setCannot(event.target.value)}
              className="min-h-14 resize-y"
            />
          </label>
        </div>
        {twin ? (
          <AppLink href={p.chatWithAgent(twin.id)} className="self-start text-caption text-primary hover:underline">
            {t(($) => $.twin_open_chat)}
          </AppLink>
        ) : null}
        {error ? <p className="text-body text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={saving}>
            {t(($) => $.cancel)}
          </Button>
          <Button size="sm" disabled={saving || !name.trim() || !can.trim()} onClick={() => void save()}>
            {t(($) => $.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
