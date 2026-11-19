import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

function issue(overrides: Record<string, unknown> = {}) {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 12,
    identifier: "ACM-12",
    title: "Investigate checkout failures",
    description: "Investigate checkout failures from yesterday",
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-11-12T08:00:00Z",
    updated_at: "2026-11-12T08:00:00Z",
    ...overrides,
  };
}

function respond(payload: unknown, status = 200) {
  return vi.fn().mockResolvedValue(
    new Response(JSON.stringify(payload), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}

describe("ApiClient dispatchWork", () => {
  it("posts the text, optional department, and idempotency key", async () => {
    const fetchMock = respond({
      issue: issue(),
      dispatch: { unit_id: "unit-2", unit_path: "Engineering/Payments" },
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.dispatchWork({
      text: "Investigate checkout failures",
      unit_id: "unit-2",
      client_request_id: "req-1",
    });

    expect(result.issue).toMatchObject({
      id: "issue-1",
      identifier: "ACM-12",
      title: "Investigate checkout failures",
    });
    expect(result.dispatch).toEqual({
      unit_id: "unit-2",
      unit_path: "Engineering/Payments",
      squad_id: "",
      personal: false,
    });
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      "https://api.example.test/api/org/dispatch",
    );
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      text: "Investigate checkout failures",
      unit_id: "unit-2",
      client_request_id: "req-1",
    });
  });

  it("sends a personal dispatch and reads the personal squad ref", async () => {
    const fetchMock = respond({
      issue: issue(),
      dispatch: {
        unit_id: "",
        unit_path: "",
        squad_id: "squad-9",
        personal: true,
      },
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.dispatchWork({
      text: "Plan my week",
      personal: true,
      client_request_id: "req-2",
    });

    expect(result.dispatch).toMatchObject({
      squad_id: "squad-9",
      personal: true,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      text: "Plan my week",
      personal: true,
      client_request_id: "req-2",
    });
  });

  it("rejects a response that carries no readable issue", async () => {
    vi.stubGlobal("fetch", respond({ issue: null, dispatch: {} }));

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.dispatchWork({ text: "Do work", client_request_id: "req-1" }),
    ).rejects.toThrow(/could not read the dispatch/);
  });
});

describe("ApiClient listMyDispatches", () => {
  it("defaults additive fields so older servers still render history", async () => {
    const fetchMock = respond({
      dispatches: [
        {
          issue: issue(),
          unit_id: "unit-1",
          dispatched_at: "2026-11-12T08:00:00Z",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const dispatches = await client.listMyDispatches();

    expect(dispatches).toHaveLength(1);
    expect(dispatches[0]).toMatchObject({
      unit_id: "unit-1",
      unit_path: "",
      squad_id: "",
      personal: false,
      dispatched_at: "2026-11-12T08:00:00Z",
      completed: false,
      leader_state: "none",
      issue: { id: "issue-1", identifier: "ACM-12" },
    });
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      "https://api.example.test/api/org/dispatches?limit=30",
    );
  });

  it("falls back to an empty history on a malformed response", async () => {
    vi.stubGlobal("fetch", respond({ dispatches: "not-an-array" }));

    const client = new ApiClient("https://api.example.test");
    await expect(client.listMyDispatches(10)).resolves.toEqual([]);
  });
});

describe("ApiClient listDepartmentDispatches", () => {
  it("reads stage, requester and blocker with defaults for older servers", async () => {
    const fetchMock = respond({
      dispatches: [
        {
          issue: issue(),
          unit_id: "unit-1",
          unit_path: "研发部 / 平台组",
          dispatched_at: "2026-11-12T08:00:00Z",
          requester_id: "user-1",
          requester_name: "Henry",
          stage: "review",
          blocker: { done: "一半", missing: "数据库权限", consequence: "无法验收" },
        },
        {
          issue: issue(),
          dispatched_at: "2026-11-12T09:00:00Z",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const dispatches = await client.listDepartmentDispatches(10);

    expect(dispatches).toHaveLength(2);
    expect(dispatches[0]).toMatchObject({
      requester_name: "Henry",
      stage: "review",
      blocker: { missing: "数据库权限" },
    });
    // Missing additive fields fall back instead of dropping the item.
    expect(dispatches[1]).toMatchObject({
      requester_id: "",
      requester_name: "",
      stage: "in_progress",
    });
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      "https://api.example.test/api/org/dispatch-queue?limit=10",
    );
  });

  it("falls back to an empty queue on a malformed response", async () => {
    vi.stubGlobal("fetch", respond({ dispatches: "not-an-array" }));

    const client = new ApiClient("https://api.example.test");
    await expect(client.listDepartmentDispatches()).resolves.toEqual([]);
  });
});

describe("ApiClient reassignDispatch", () => {
  it("posts the target department and reads the moved dispatch", async () => {
    const fetchMock = respond({
      issue: issue({ assignee_type: "squad", assignee_id: "squad-2" }),
      dispatch: { unit_id: "unit-2", unit_path: "研发部 / 平台组", squad_id: "squad-2" },
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.reassignDispatch("issue-1", "unit-2");

    expect(result.dispatch).toMatchObject({ unit_id: "unit-2", squad_id: "squad-2" });
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(String(url)).toBe("https://api.example.test/api/org/dispatches/issue-1/reassign");
    expect(init).toMatchObject({ method: "POST" });
    expect(JSON.parse(String(init?.body))).toEqual({ unit_id: "unit-2" });
  });

  it("rejects a response without a readable dispatch", async () => {
    vi.stubGlobal("fetch", respond({ issue: null }));

    const client = new ApiClient("https://api.example.test");
    await expect(client.reassignDispatch("issue-1", "unit-2")).rejects.toThrow(
      "could not read the dispatch",
    );
  });
});
