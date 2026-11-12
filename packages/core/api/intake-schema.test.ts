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

  it("keeps the approval status and folds unknown statuses into waiting", () => {
    const parse = (status: string) =>
      IntakeListItemSchema.parse({ issue_id: "i", body: "b", display_status: status }).display_status;
    expect(parse("待审批")).toBe("待审批");
    expect(parse("未来的新状态")).toBe("等待处理");
  });
});
