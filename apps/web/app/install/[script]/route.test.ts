// @vitest-environment node
import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";
import { GET } from "./route";

// The request URL deliberately carries the container bind address, matching
// what the standalone server produces in the deployed image.
function get(
  script: string,
  headers: Record<string, string> = {},
): Promise<Response> {
  return GET(
    new NextRequest(`http://0.0.0.0/install/${script}`, { headers }),
    { params: Promise.resolve({ script }) },
  );
}

describe("GET /install/[script]", () => {
  it("substitutes the origin the client used, not the bind address", async () => {
    // Regression: the route trusted `request.nextUrl.origin`, which in the
    // container is `http://0.0.0.0` — the install script would then download
    // release assets from a dead address.
    const response = await get("install.sh", {
      host: "172.30.11.181:31383",
    });

    expect(response.status).toBe(200);
    const body = await response.text();
    expect(body).toContain('ORIGIN="http://172.30.11.181:31383"');
    expect(body).not.toContain("__MULTICA_ORIGIN__");
  });

  it("prefers the forwarded pair behind a reverse proxy", async () => {
    const response = await get("install.ps1", {
      host: "0.0.0.0",
      "x-forwarded-host": "multica.example.com",
      "x-forwarded-proto": "https",
    });

    expect(response.status).toBe(200);
    expect(await response.text()).toContain(
      '$Origin = "https://multica.example.com"',
    );
  });

  it("refuses to serve a script that would point at the bind address", async () => {
    const response = await get("install.sh", { host: "0.0.0.0" });

    expect(response.status).toBe(500);
  });

  it("404s unknown scripts", async () => {
    const response = await get("install.cmd", { host: "multica.example.com" });

    expect(response.status).toBe(404);
  });
});
