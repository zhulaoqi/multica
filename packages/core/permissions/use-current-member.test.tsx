/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { createAuthStore, registerAuthStore } from "../auth";
import type { ApiClient } from "../api/client";
import type { MemberWithUser, User } from "../types";
import { workspaceKeys } from "../workspace/queries";
import { useCurrentMember } from "./use-current-member";

const WS = "ws-1";
const USER = "u-1";

const user: User = {
  id: USER,
  name: "Ada",
  email: "ada@example.com",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  language: null,
  profile_description: "",
  timezone: null,
  is_operator: false,
  created_at: "",
  updated_at: "",
};

function member(overrides: Partial<MemberWithUser> = {}): MemberWithUser {
  return {
    id: "m-1",
    workspace_id: WS,
    user_id: USER,
    role: "member",
    created_at: "",
    name: "Ada",
    email: "ada@example.com",
    avatar_url: null,
    org_unit_id: "unit-1",
    is_org_lead: false,
    ...overrides,
  };
}

function renderWith(qc: QueryClient) {
  return renderHook(() => useCurrentMember(WS), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
}

describe("useCurrentMember department scope", () => {
  let qc: QueryClient;

  beforeEach(() => {
    const auth = createAuthStore({
      api: {} as ApiClient,
      storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    });
    registerAuthStore(auth);
    auth.setState({ user });
    qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("treats a plain member as neither admin nor manager", () => {
    qc.setQueryData(workspaceKeys.members(WS), [member()]);
    const { result } = renderWith(qc);

    expect(result.current.role).toBe("member");
    expect(result.current.effectiveRole).toBe("member");
    expect(result.current.isAdmin).toBe(false);
    expect(result.current.isManager).toBe(false);
  });

  it("gives the department lead and stored owner the same management rights", () => {
    qc.setQueryData(workspaceKeys.members(WS), [member({ is_org_lead: true })]);
    const { result } = renderWith(qc);

    expect(result.current.isOrgLead).toBe(true);
    expect(result.current.isManager).toBe(true);
    expect(result.current.isAdmin).toBe(false);
    expect(result.current.isOwner).toBe(false);
    // Resource rules read effectiveRole, so the lead can manage project
    // resources without owning the stored owner role.
    expect(result.current.effectiveRole).toBe("admin");
  });

  it("never widens a stored owner below the owner role", () => {
    qc.setQueryData(workspaceKeys.members(WS), [member({ role: "owner", is_org_lead: true })]);
    const { result } = renderWith(qc);

    expect(result.current.effectiveRole).toBe("owner");
    expect(result.current.isOwner).toBe(true);
    expect(result.current.isAdmin).toBe(true);
  });

  it("defaults the lead flag off when an older server omits it", () => {
    qc.setQueryData(workspaceKeys.members(WS), [
      { ...member(), is_org_lead: undefined },
    ]);
    const { result } = renderWith(qc);

    expect(result.current.isOrgLead).toBe(false);
    expect(result.current.isManager).toBe(false);
  });
});
