// @vitest-environment node

import { afterEach, describe, expect, it, vi } from "vitest";
import { newRequestID } from "./request-id";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe("newRequestID", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("uses crypto.randomUUID when the context is secure", () => {
    vi.stubGlobal("crypto", { randomUUID: () => "11111111-1111-4111-8111-111111111111" });
    expect(newRequestID()).toBe("11111111-1111-4111-8111-111111111111");
  });

  it("builds a v4 uuid from getRandomValues when randomUUID is missing", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array) => {
        bytes.fill(0xff);
        return bytes;
      },
    });
    const id = newRequestID();
    expect(id).toBe("ffffffff-ffff-4fff-bfff-ffffffffffff");
    expect(id).toMatch(UUID_V4);
  });

  it("still returns a v4 uuid without any web crypto", () => {
    vi.stubGlobal("crypto", undefined);
    expect(newRequestID()).toMatch(UUID_V4);
  });
});
