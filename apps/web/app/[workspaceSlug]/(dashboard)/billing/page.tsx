import { redirect } from "next/navigation";

// Billing is disabled in this fork; the upstream API test page is not part of
// the internal product. Any saved link lands back on the workspace instead.
export default async function BillingRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(`/${workspaceSlug}`);
}
