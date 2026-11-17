// @vitest-environment node

import { describe, expect, it } from "vitest";
import { ApiError } from "../api";
import { parseDispatchCandidates } from "./queries";
import { DispatchListItemSchema, DispatchWorkResponseSchema } from "./schemas";

describe("parseDispatchCandidates", () => {
  it("reads candidates from an ambiguous routing error", () => {
    const error = new ApiError("ambiguous", 422, "Unprocessable Entity", {
      code: "org_dispatch_ambiguous",
      candidates: [
        { unit_id: "unit-1", path: "Engineering/Payments", agent_count: 3 },
        { unit_id: "unit-2", path: "Engineering/Infra", agent_count: 1 },
      ],
    });

    expect(parseDispatchCandidates(error)).toEqual([
      { unit_id: "unit-1", path: "Engineering/Payments", agent_count: 3 },
      { unit_id: "unit-2", path: "Engineering/Infra", agent_count: 1 },
    ]);
  });

  it("defaults missing candidate fields instead of dropping the candidate", () => {
    const error = new ApiError("ambiguous", 422, "Unprocessable Entity", {
      candidates: [{ unit_id: "unit-1" }],
    });

    expect(parseDispatchCandidates(error)).toEqual([
      { unit_id: "unit-1", path: "", agent_count: 0 },
    ]);
  });

  it("returns nothing for unrelated errors or malformed bodies", () => {
    expect(parseDispatchCandidates(new Error("network"))).toEqual([]);
    expect(
      parseDispatchCandidates(
        new ApiError("server", 500, "Internal Server Error", { code: "org_dispatch_failed" }),
      ),
    ).toEqual([]);
    expect(
      parseDispatchCandidates(
        new ApiError("ambiguous", 422, "Unprocessable Entity", {
          candidates: [{ agent_count: 2 }],
        }),
      ),
    ).toEqual([]);
  });
});

describe("workbench schemas", () => {
  it("keeps unknown leader states so the page can fall back", () => {
    const listItem = DispatchListItemSchema.pick({
      leader_state: true,
      completed: true,
      unit_path: true,
    });
    expect(listItem.parse({ leader_state: "paused" })).toEqual({
      leader_state: "paused",
      completed: false,
      unit_path: "",
    });
  });

  it("rejects a dispatch response without an issue", () => {
    const result = DispatchWorkResponseSchema.safeParse({
      issue: null,
      dispatch: { unit_id: "unit-1" },
    });
    expect(result.success).toBe(false);
  });
});
