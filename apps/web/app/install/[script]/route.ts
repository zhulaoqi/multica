import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { NextRequest } from "next/server";

// The CLI installer scripts live next to the app, not in `public/`, because
// they are served with the request origin substituted in: the shell command
// the app shows points at this deployment, and the script then downloads its
// release assets from the same origin instead of a public code host.
const SCRIPTS = {
  "install.sh": {
    file: "install.sh",
    contentType: "text/x-shellscript; charset=utf-8",
  },
  "install.ps1": {
    file: "install.ps1",
    contentType: "text/plain; charset=utf-8",
  },
} as const;

const ORIGIN_PLACEHOLDER = "__MULTICA_ORIGIN__";

// The standalone server binds `HOSTNAME=0.0.0.0` and builds request URLs from
// it, so `request.nextUrl.origin` is the container's bind address, not the
// address the client used. Take the origin from the request headers instead
// (proxies in front of the pod set the forwarded pair).
const BIND_ADDRESSES = new Set(["0.0.0.0", "::", "[::]"]);

function requestOrigin(request: NextRequest): string | null {
  const host =
    request.headers.get("x-forwarded-host") ?? request.headers.get("host");
  const proto =
    request.headers.get("x-forwarded-proto") ??
    request.nextUrl.protocol.replace(/:$/, "");
  if (!host || (proto !== "http" && proto !== "https")) return null;
  let url: URL;
  try {
    url = new URL(`${proto}://${host}`);
  } catch {
    return null;
  }
  if (BIND_ADDRESSES.has(url.hostname)) return null;
  return url.origin;
}

// `next start` from the app directory and the standalone server started from
// the deployment root resolve the scripts differently; try both, in order.
function scriptDirs(): string[] {
  return [
    join(process.cwd(), "install-scripts"),
    join(process.cwd(), "apps", "web", "install-scripts"),
  ];
}

async function readScript(file: string): Promise<string | null> {
  for (const dir of scriptDirs()) {
    try {
      return await readFile(join(dir, file), "utf8");
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
    }
  }
  return null;
}

export async function GET(
  request: NextRequest,
  context: { params: Promise<{ script: string }> },
) {
  const { script } = await context.params;
  const entry = SCRIPTS[script as keyof typeof SCRIPTS];
  if (!entry) {
    return new Response("Not found\n", { status: 404 });
  }

  let template: string | null;
  try {
    template = await readScript(entry.file);
  } catch (err) {
    console.error(`[install] could not read ${entry.file}:`, err);
    return new Response("Install script unavailable\n", { status: 500 });
  }
  if (template === null) {
    console.error(`[install] ${entry.file} is missing from the image`);
    return new Response("Install script unavailable\n", { status: 500 });
  }

  const origin = requestOrigin(request);
  if (!origin) {
    console.error("[install] could not determine the request origin");
    return new Response("Install script unavailable\n", { status: 500 });
  }
  return new Response(template.replaceAll(ORIGIN_PLACEHOLDER, origin), {
    headers: {
      "content-type": entry.contentType,
      "cache-control": "no-store",
    },
  });
}
