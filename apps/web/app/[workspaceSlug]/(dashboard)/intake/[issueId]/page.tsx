"use client";

import { use } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { IntakePage } from "@multica/views/intake";

export default function Page({
  params,
}: {
  params: Promise<{ issueId: string }>;
}) {
  const { issueId } = use(params);
  const workspace = useCurrentWorkspace();
  return <IntakePage focusIssueId={issueId} workspaceName={workspace?.name} />;
}
