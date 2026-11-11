// @vitest-environment node
import { describe, expect, it } from "vitest";

import { parseWithFallback } from "./schema";
import {
  EMPTY_INTAKE_SUBMIT_RESPONSE,
  IntakeListItemSchema,
  IntakeSubmitResponseSchema,
} from "./intake-schema";

describe("intake response schemas", () => {
  it("keeps a malformed submit body from inventing an issue", () => {
    expect(
      parseWithFallback({ issue_id: 12 }, IntakeSubmitResponseSchema, EMPTY_INTAKE_SUBMIT_RESPONSE, {
        endpoint: "POST /api/intake/requests",
      }),
    ).toEqual(EMPTY_INTAKE_SUBMIT_RESPONSE);
  });

  it("drops a list that is not an array of requests", () => {
    expect(
      parseWithFallback({ items: [] }, IntakeListItemSchema.array(), [], {
        endpoint: "GET /api/intake/requests",
      }),
    ).toEqual([]);
  });
});
