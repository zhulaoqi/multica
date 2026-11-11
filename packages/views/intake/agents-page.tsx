import type { IntakeAgent } from "@multica/core/api";
import { AppLink } from "../navigation";

export function AgentRoster({
  agents,
  slug,
  onSave,
}: {
  agents: IntakeAgent[];
  slug: string;
  onSave: (agent: IntakeAgent, can: string, cannot: string) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="text-sm">我的 Agent</div>
      {agents.map((agent) => (
        <AgentRow key={agent.id} agent={agent} slug={slug} onSave={(can, cannot) => onSave(agent, can, cannot)} />
      ))}
    </div>
  );
}

function AgentRow({
  agent,
  slug,
  onSave,
}: {
  agent: IntakeAgent;
  slug: string;
  onSave: (can: string, cannot: string) => void;
}) {
  const where = agent.runtime_mode === "cloud" ? "云上" : "本机";
  return (
    <div className="flex flex-col gap-2 rounded-md border px-3 py-2 text-sm">
      <div>{agent.name} · {where} · {agent.online ? "在线" : "不在线"} · 正在做 {agent.active_count ?? 0} 条</div>
      {(agent.placements ?? []).map((place) => (
        <div key={`${place.project}-${place.module}-${place.role}`}>{place.project} / {place.product} / {place.module} · {place.role}</div>
      ))}
      <button type="button" onClick={() => onSave(agent.can ?? "", agent.cannot ?? "")}>保存范围</button>
      {slug ? <AppLink href={`/${slug}/agents/${agent.id}`}>调试</AppLink> : null}
    </div>
  );
}
