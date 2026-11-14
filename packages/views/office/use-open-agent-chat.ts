"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, sortChatSessions } from "@multica/core/chat/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";

/**
 * Opens the chat with an agent from the office. Resumes the agent's most
 * recent active session when there is one (same as picking it in the chat
 * list); otherwise it lands on a fresh compose for that agent.
 *
 * Uses the floating window when it is available and falls back to the chat tab
 * when the user turned the overlay off in Settings.
 */
export function useOpenAgentChat(wsId: string): (agentId: string) => void {
  const { data: sessions = [] } = useQuery({
    ...chatSessionsOptions(wsId),
    enabled: !!wsId,
  });
  const paths = useWorkspacePaths();
  const { push } = useNavigation();

  return useCallback(
    (agentId: string) => {
      const latest = sortChatSessions(
        sessions.filter(
          (session) => session.agent_id === agentId && session.status === "active",
        ),
      )[0];

      const store = useChatStore.getState();
      store.setSelectedAgentId(agentId);
      store.setActiveSession(latest?.id ?? null);

      if (store.floatingChatEnabled) {
        store.setOpen(true);
        return;
      }
      push(
        latest
          ? `${paths.chat()}?session=${encodeURIComponent(latest.id)}`
          : `${paths.chat()}?agent=${encodeURIComponent(agentId)}`,
      );
    },
    [sessions, paths, push],
  );
}
