"use client";

import { useEffect, useId, useRef, useState } from "react";
import { ArrowUpRight, ListTodo, MessageSquare, X } from "@multica/ui/icons";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import type { Agent } from "@multica/core/types";
import { useT } from "../../i18n";
import type { AgentDeskStatus } from "../scene/characters";
import { isTypingTarget } from "../scene/input";
import { AgentStatusBadge, initials } from "./agent-status";
import { AgentTaskComposer } from "./agent-task-composer";

export interface AgentInteractPanelProps {
  agent: Agent;
  status: AgentDeskStatus;
  /** False for agents the current member may not assign work to. */
  canAssign: boolean;
  onClose: () => void;
  onOpenChat: () => void;
  onOpenDetail: () => void;
  /** A quick task was accepted; the world shows the agent's reaction. */
  onTaskCreated: () => void;
  /** Hand the typed brief to the full create dialog. */
  onMoreOptionsTask: (prompt: string) => void;
  className?: string;
}

/**
 * Non-modal dock shown while the player stands next to an agent. The world
 * keeps running behind it, so movement keys stay with the scene and only the
 * panel's own shortcuts (T chat, Q task, Esc close) are captured here.
 */
export function AgentInteractPanel({
  agent,
  status,
  canAssign,
  onClose,
  onOpenChat,
  onOpenDetail,
  onTaskCreated,
  onMoreOptionsTask,
  className,
}: AgentInteractPanelProps) {
  const { t } = useT("office");
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const [composing, setComposing] = useState(false);

  useEffect(() => {
    panelRef.current?.focus();
    setComposing(false);
  }, [agent.id]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) {
        // Escape still closes; other keys belong to the field.
        if (event.key === "Escape") {
          event.preventDefault();
          if (composing) setComposing(false);
          else onClose();
        }
        return;
      }
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      if (key === "t") {
        event.preventDefault();
        onOpenChat();
      } else if (key === "q" && canAssign) {
        event.preventDefault();
        setComposing((open) => !open);
      } else if (event.key === "Escape") {
        event.preventDefault();
        if (composing) setComposing(false);
        else onClose();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [canAssign, composing, onClose, onOpenChat]);

  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-labelledby={titleId}
      tabIndex={-1}
      className={cn(
        "pointer-events-auto flex w-80 max-w-[calc(100%-1.5rem)] flex-col gap-3 rounded-xl border border-border/60 bg-surface-raised p-3 shadow-lg outline-none",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <ActorAvatar
          name={agent.name}
          initials={initials(agent.name)}
          isAgent
          size="sm"
        />
        <div className="min-w-0 flex-1">
          <p id={titleId} className="truncate text-body font-medium">
            {agent.name}
          </p>
          <AgentStatusBadge status={status} />
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.page.interact.close)}
          onClick={onClose}
        >
          <X />
        </Button>
      </div>

      {composing ? (
        <AgentTaskComposer
          agent={agent}
          onCreated={() => {
            setComposing(false);
            onTaskCreated();
          }}
          onMoreOptions={onMoreOptionsTask}
          onCancel={() => setComposing(false)}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-1.5">
          <Button type="button" size="sm" onClick={onOpenChat}>
            <MessageSquare />
            {t(($) => $.page.interact.chat)}
            <Kbd>T</Kbd>
          </Button>
          {canAssign && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setComposing(true)}
            >
              <ListTodo />
              {t(($) => $.page.interact.task)}
              <Kbd>Q</Kbd>
            </Button>
          )}
          <Button type="button" size="sm" variant="ghost" onClick={onOpenDetail}>
            {t(($) => $.page.interact.detail)}
            <ArrowUpRight />
          </Button>
        </div>
      )}
    </div>
  );
}
