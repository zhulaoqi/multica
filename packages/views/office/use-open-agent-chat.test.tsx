import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types/chat";
import { useOpenAgentChat } from "./use-open-agent-chat";

const push = vi.hoisted(() => vi.fn());
const store = vi.hoisted(() => ({
  floatingChatEnabled: true,
  selectedAgentId: null as string | null,
  activeSessionId: null as string | null,
  setSelectedAgentId: vi.fn(),
  setActiveSession: vi.fn(),
  setOpen: vi.fn(),
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: { getState: () => store },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/workspace-test/chat" }),
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push }),
}));

const WS = "ws-test";

function session(overrides: Partial<ChatSession>): ChatSession {
  return {
    id: "session-1",
    workspace_id: WS,
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "",
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderHookWithSessions(sessions: ChatSession[]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(chatSessionsOptions(WS).queryKey, sessions);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return renderHook(() => useOpenAgentChat(WS), { wrapper });
}

describe("useOpenAgentChat", () => {
  beforeEach(() => {
    push.mockClear();
    store.floatingChatEnabled = true;
    store.selectedAgentId = null;
    store.activeSessionId = null;
    store.setSelectedAgentId.mockClear();
    store.setActiveSession.mockClear();
    store.setOpen.mockClear();
  });

  it("resumes the agent's most recent active session in the floating window", () => {
    const { result } = renderHookWithSessions([
      session({ id: "older", updated_at: "2026-01-01T00:00:00Z" }),
      session({ id: "newer", updated_at: "2026-02-01T00:00:00Z" }),
      session({
        id: "archived",
        status: "archived",
        updated_at: "2026-03-01T00:00:00Z",
      }),
    ]);

    result.current("agent-1");

    expect(store.setSelectedAgentId).toHaveBeenCalledWith("agent-1");
    expect(store.setActiveSession).toHaveBeenCalledWith("newer");
    expect(store.setOpen).toHaveBeenCalledWith(true);
    expect(push).not.toHaveBeenCalled();
  });

  it("starts a fresh compose when the agent has no active session", () => {
    const { result } = renderHookWithSessions([]);

    result.current("agent-1");

    expect(store.setActiveSession).toHaveBeenCalledWith(null);
    expect(store.setOpen).toHaveBeenCalledWith(true);
  });

  it("falls back to the chat tab when the floating window is off", () => {
    store.floatingChatEnabled = false;
    const { result } = renderHookWithSessions([
      session({ id: "session-9", updated_at: "2026-02-01T00:00:00Z" }),
    ]);

    result.current("agent-1");

    expect(store.setOpen).not.toHaveBeenCalled();
    expect(push).toHaveBeenCalledWith("/workspace-test/chat?session=session-9");
  });

  it("deep-links a fresh compose when there is no session and no overlay", () => {
    store.floatingChatEnabled = false;
    const { result } = renderHookWithSessions([]);

    result.current("agent-1");

    expect(push).toHaveBeenCalledWith("/workspace-test/chat?agent=agent-1");
  });
});
