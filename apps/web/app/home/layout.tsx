"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useWorkspaceList } from "@multica/core/workspace";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";

/**
 * The C-end lives outside the workspace routes but still needs a workspace
 * identity for its requests, so this layout picks one: the organization, or
 * the first workspace before an organization exists. The admin side is
 * reached from the account menu, not from here.
 */
export default function HomeLayout({ children }: { children: React.ReactNode }) {
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const router = useRouter();
  const { workspaces, ready } = useWorkspaceList({ enabled: !!user });

  useEffect(() => {
    if (!isAuthLoading && !user) router.replace(paths.login());
  }, [isAuthLoading, user, router]);

  useEffect(() => {
    if (user && user.onboarded_at == null) router.replace(paths.onboarding());
  }, [user, router]);

  useEffect(() => {
    if (user && ready && workspaces.length === 0) router.replace(paths.newWorkspace());
  }, [user, ready, workspaces.length, router]);

  const workspace = workspaces.find((w) => w.is_organization) ?? workspaces[0];
  // Same singleton write as the workspace layout, so the first request on
  // this route already carries the X-Workspace-ID header.
  if (workspace) setCurrentWorkspace(workspace.slug, workspace.id);

  if (isAuthLoading || (user && !ready)) {
    return (
      <div className="flex h-svh items-center justify-center">
        <MulticaIcon mode="launch" size={40} />
      </div>
    );
  }
  if (!user || !workspace) return null;

  return (
    <WorkspaceSlugProvider slug={workspace.slug}>{children}</WorkspaceSlugProvider>
  );
}
