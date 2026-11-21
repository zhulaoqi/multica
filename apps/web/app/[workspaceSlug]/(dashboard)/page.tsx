"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { paths, useCurrentWorkspace } from "@multica/core/paths";

/**
 * Workspace landing. The organization workspace administers the company, so
 * it opens on the org chart; every other workspace is a project, which opens
 * on its issues. The client picks the destination because only the workspace
 * list knows which kind this slug is.
 */
export default function Page() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const workspace = useCurrentWorkspace();
  const router = useRouter();

  useEffect(() => {
    if (!workspace) return;
    const p = paths.workspace(workspaceSlug);
    router.replace(workspace.is_organization ? p.org() : p.issues());
  }, [workspace, workspaceSlug, router]);

  return (
    <div className="flex h-svh items-center justify-center">
      <MulticaIcon mode="launch" size={40} />
    </div>
  );
}
