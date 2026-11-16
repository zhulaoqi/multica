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
