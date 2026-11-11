"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { IntakePage } from "@multica/views/intake";

export default function Page() {
  const workspace = useCurrentWorkspace();
  return <IntakePage initialMode="manage" screen="scheduled" workspaceName={workspace?.name} />;
}
