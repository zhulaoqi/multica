export function RoutesHeading({ canMaintain }: { canMaintain: boolean }) {
  if (!canMaintain) return null;
  return <div className="text-sm">能力总览</div>;
}
