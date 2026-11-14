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

  const origin = request.nextUrl.origin.replace(/\/+$/, "");
  return new Response(template.replaceAll(ORIGIN_PLACEHOLDER, origin), {
    headers: {
      "content-type": entry.contentType,
      "cache-control": "no-store",
    },
  });
}
