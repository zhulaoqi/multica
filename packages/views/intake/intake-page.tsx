"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, errorCode, type IntakeAgent, type IntakeMaintainProject, type IntakeModule, type IntakeProjectItem } from "@multica/core/api";
import { useWorkspaceSlug } from "@multica/core/paths";
import { createSafeId } from "@multica/core/utils";
import { useOptionalWS } from "@multica/core/realtime";
import type { WSEventType } from "@multica/core/types";
import { AppLink } from "../navigation";
import { HelpPanel } from "./help-panel";
import { IntakeBar } from "./intake-bar";
import { ScheduledList } from "./scheduled-page";
import { RoutesHeading } from "./routes-page";

export function IntakePage({
  focusIssueId,
  workspaceName,
  initialMode = "chat",
  screen = "all",
}: {
  focusIssueId?: string;
  workspaceName?: string;
  initialMode?: "chat" | "manage";
  screen?: "all" | "scheduled" | "agents" | "routes";
} = {}) {
  const [mode, setMode] = useState<"chat" | "manage">(initialMode);
  const [text, setText] = useState("");
  const [startAt, setStartAt] = useState("");
  const [status, setStatus] = useState("");
  const [sending, setSending] = useState(false);
  const [requestID, setRequestID] = useState(() => createSafeId());
  const [canMaintain, setCanMaintain] = useState(false);
  const [maintainProjects, setMaintainProjects] = useState<IntakeMaintainProject[]>([]);
  const [catalog, setCatalog] = useState<IntakeAgent[]>([]);
  const [projectId, setProjectId] = useState("");
  const [product, setProduct] = useState("");
  const [moduleName, setModuleName] = useState("");
  const [scope, setScope] = useState("");
  const [agentId, setAgentId] = useState("");
  const [backupAgentId, setBackupAgentId] = useState("");
  const [agentName, setAgentName] = useState("");
  const [agentCan, setAgentCan] = useState("");
  const [agentCannot, setAgentCannot] = useState("");
  const [memberIDs, setMemberIDs] = useState("");
  const [testURL, setTestURL] = useState("");
  const [person, setPerson] = useState("");
  const [menu, setMenu] = useState(false);
  const [modules, setModules] = useState<IntakeModule[]>([]);
  const [agents, setAgents] = useState<IntakeAgent[]>([]);
  const [note, setNote] = useState("");
  const [help, setHelp] = useState(false);
  const [sessionReady, setSessionReady] = useState(false);
  const wsKey = useWorkspaceSlug() ?? "session";
  const queryClient = useQueryClient();
  const mine = useQuery({
    queryKey: ["intake", wsKey, "mine"],
    queryFn: () => api.listMyIntake(),
  });
  const scheduled = useQuery({
    queryKey: ["intake-scheduled", wsKey],
    queryFn: () => api.listScheduledIntake(),
    enabled: mode === "manage",
  });
  const projects = useQuery({
    queryKey: ["intake", wsKey],
    queryFn: () => api.listProjectIntake(),
    enabled: mode === "manage",
  });
  const items = mine.data ?? [];
  const [clock, setClock] = useState(() => Date.now());
  useEffect(() => {
    const ticking = items.some((item) => item.run?.status === "running" && item.run.started_at);
    if (!ticking) return;
    const timer = window.setInterval(() => setClock(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, [items]);

  useEffect(() => {
    if (!focusIssueId) return;
    const node = document.getElementById(`intake-${focusIssueId}`);
    if (!node || typeof node.scrollIntoView !== "function") return;
    try {
      node.scrollIntoView({ block: "center" });
    } catch {
      // The test renderer has no layout engine.
    }
  }, [focusIssueId, items]);

  const refresh = useCallback(() => {
    return Promise.all([
      queryClient.invalidateQueries({ queryKey: ["intake"] }),
      queryClient.invalidateQueries({ queryKey: ["intake-scheduled"] }),
    ]);
  }, [queryClient]);
  const onIntakeEvent = useCallback(() => {
    void refresh();
  }, [refresh]);
  const live = useOptionalWS();
  useEffect(() => {
    if (!live) return;
    const events: WSEventType[] = ["issue:updated", "task:queued", "task:running", "task:completed", "task:failed", "task:cancelled"];
    const unsubs = events.map((event) => live.subscribe(event, onIntakeEvent));
    return () => unsubs.forEach((unsub) => unsub());
  }, [live, onIntakeEvent]);

  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("mode") === "manage") setMode("manage");
    void api.intakeSession().then((session) => {
      setPerson(session.name);
      setCanMaintain(session.can_maintain === true);
      setSessionReady(true);
    }).catch(() => {
      setPerson("");
      setSessionReady(true);
    });
    if (mode === "manage") {
      void api.listIntakeAgents().then(setAgents).catch(() => setAgents([]));
    }
  }, [mode]);

  useEffect(() => {
    if (mode !== "manage" || !canMaintain) return;
    void api.listMaintainProjects().then(setMaintainProjects).catch(() => setMaintainProjects([]));
    void api.listAssignableAgents().then(setCatalog).catch(() => setCatalog([]));
  }, [mode, canMaintain]);

  async function send() {
    const body = text.trim();
    if (!body || sending) return;
    setSending(true);
    setStatus("");
    try {
      const result = await api.submitIntake({
        body,
        client_request_id: requestID,
        start_at: startAt ? new Date(startAt).toISOString() : undefined,
      });
      if (!result.issue_id) {
        setStatus("没对上，请再补一句");
        return;
      }
      setText("");
      setRequestID(createSafeId());
      setStatus(result.enqueue_error ? "等待处理" : "正在做");
      await refresh();
    } catch (err) {
      const code = errorCode(err);
      if (code === "intake_project_lead_missing") {
        setStatus("还没有可选项目");
        return;
      }
      if (code === "intake_split_empty") {
        setStatus("没对上，请再补一句");
        return;
      }
      setStatus("没发出去");
    } finally {
      setSending(false);
    }
  }

  async function decide(issueId: string, action: "continue" | "stop" | "accept" | "cancel" | "resume" | "match") {
    const row = items.find((item) => item.issue_id === issueId) ?? projects.data?.find((item) => item.issue_id === issueId);
    if (action === "continue" && note.trim() === "" && row?.progress?.remain !== "执行没有进展") {
      setStatus("请先写补充");
      return;
    }
    try {
      await api.decideIntake(issueId, action, note.trim());
      setNote("");
      setStatus("");
      await refresh();
    } catch {
      setStatus("没提交成功");
    }
  }

  async function loadModules() {
    if (!projectId) return;
    try {
      setModules(await api.listIntakeModules(projectId));
      setStatus("");
    } catch {
      setStatus("模块没加载");
    }
  }

  async function saveModules(next: IntakeModule[]) {
    if (!projectId) return;
    setStatus("");
    try {
      await api.replaceIntakeModules({
        project_id: projectId,
        modules: next.map((module, position) => ({
          product: module.product,
          module: module.module,
          scope: module.scope,
          agent_id: module.agent_id,
          backup_agent_id: module.backup_agent_id,
          position,
        })),
      });
      setStatus("已保存");
      return true;
    } catch {
      setStatus("没保存");
      return false;
    }
  }

  async function saveModule() {
    if (!projectId || !product || !moduleName || !scope || !agentId) return;
    const next = [...modules, { id: "", product, module: moduleName, scope, agent_id: agentId, backup_agent_id: backupAgentId || undefined }];
    if (!(await saveModules(next))) return;
    setModules(next);
    setProduct("");
    setModuleName("");
    setScope("");
    setAgentId("");
  }

  async function registerAgent() {
    if (!agentName || !agentCan) return;
    try {
      await api.registerIntakeAgent({ name: agentName, can: agentCan, cannot: agentCannot, runtime_mode: "local" });
      setStatus("Agent 已登记");
    } catch {
      setStatus("Agent 没登记");
    }
  }

  async function saveMembers() {
    if (!projectId) return;
    const userIDs = memberIDs.split(/[\s,]+/).map((id) => id.trim()).filter(Boolean);
    try {
      await api.replaceIntakeMembers(projectId, userIDs);
      setStatus("成员已保存");
    } catch {
      setStatus("成员没保存");
    }
  }

  async function saveEnvironment() {
    if (!projectId || !testURL.trim()) return;
    try {
      await api.setIntakeTestEnvironment(projectId, testURL.trim());
      setStatus("测试环境已保存");
    } catch {
      setStatus("测试环境没保存");
    }
  }

  async function signOut() {
    await api.logout();
    window.location.href = "/login";
  }

  return (
    <div className="mx-auto flex h-full w-full max-w-3xl flex-col gap-4 px-6 py-8">
      <IntakeBar
        person={person}
        workspaceName={workspaceName}
        mode={mode}
        menuOpen={menu}
        onHelp={() => setHelp(true)}
        onMenu={() => setMenu((open) => !open)}
        onMode={() => setMode(mode === "chat" ? "manage" : "chat")}
        onSignOut={() => void signOut()}
        showMode={screen === "all"}
      />
      {help ? <HelpPanel canMaintain={canMaintain} onClose={() => setHelp(false)} /> : null}
      <div key={mode} className="intake-mode flex flex-col gap-4">
      {screen === "scheduled" || screen === "agents" || screen === "routes" || mode === "manage" ? (
        <div className="flex flex-col gap-2">
          {screen === "all" || screen === "scheduled" ? (
          <ScheduledList items={scheduled.data ?? []} onCancel={(issueId) => void decide(issueId, "cancel")} />
          ) : null}
          {screen === "all" ? (
            <>
          <div className="text-sm">全部需求</div>
          <ProjectRequests items={projects.data ?? []} onDecide={(issueId, action) => void decide(issueId, action)} note={note} onNote={setNote} />
            </>
          ) : null}
          {screen === "all" || screen === "agents" ? (
            <>
          <div className="text-sm">我的 Agent</div>
          <input value={agentName} onChange={(event) => setAgentName(event.target.value)} placeholder="Agent 名称" className="h-9 rounded-md border px-3 text-sm" />
          <input value={agentCan} onChange={(event) => setAgentCan(event.target.value)} placeholder="负责什么" className="h-9 rounded-md border px-3 text-sm" />
          <input value={agentCannot} onChange={(event) => setAgentCannot(event.target.value)} placeholder="不接什么" className="h-9 rounded-md border px-3 text-sm" />
          <button type="button" onClick={() => void registerAgent()} className="h-9 w-fit rounded-md border px-4 text-sm">登记 Agent</button>
          {agents.map((agent) => (
            <OwnAgent key={agent.id} agent={agent} slug={wsKey === "session" ? "" : wsKey} onSave={async (can, cannot) => {
              await api.patchIntakeAgent(agent.id, {
                name: agent.name,
                can,
                cannot,
                runtime_mode: agent.runtime_mode === "cloud" ? "cloud" : "local",
              });
              setAgents(await api.listIntakeAgents());
            }} />
          ))}
            </>
          ) : null}
          {screen === "routes" && sessionReady && !canMaintain ? <div className="text-sm">没有权限</div> : null}
          {(screen === "all" || screen === "routes") && canMaintain ? (
            <>
              <RoutesHeading canMaintain={canMaintain} />
              <select value={projectId} onChange={(event) => { setProjectId(event.target.value); setModules([]); }} className="h-9 rounded-md border px-3 text-sm">
                <option value="">选择项目</option>
                {maintainProjects.map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
              </select>
              <button type="button" onClick={() => void loadModules()} className="h-9 w-fit rounded-md border px-4 text-sm">查看模块</button>
              <CapabilityMap
                modules={modules}
                catalog={catalog}
                onChange={(next) => { setModules(next); void saveModules(next); }}
              />
              <input value={product} onChange={(event) => setProduct(event.target.value)} placeholder="产品" className="h-9 rounded-md border px-3 text-sm" />
              <input value={moduleName} onChange={(event) => setModuleName(event.target.value)} placeholder="模块" className="h-9 rounded-md border px-3 text-sm" />
              <input value={scope} onChange={(event) => setScope(event.target.value)} placeholder="边界" className="h-9 rounded-md border px-3 text-sm" />
              <select value={agentId} onChange={(event) => setAgentId(event.target.value)} className="h-9 rounded-md border px-3 text-sm">
                <option value="">主责</option>
                {catalog.map((agent) => <option key={agent.id} value={agent.id}>{agent.name} · {agent.online ? "在线" : "不在线"}</option>)}
              </select>
              <button type="button" onClick={() => void saveModule()} className="h-9 w-fit rounded-md bg-[var(--primary)] px-4 text-sm text-[var(--primary-foreground)]">加上这个模块</button>
              <input value={memberIDs} onChange={(event) => setMemberIDs(event.target.value)} placeholder="成员用户 ID，用逗号分开" className="h-9 rounded-md border px-3 text-sm" />
              <button type="button" onClick={() => void saveMembers()} className="h-9 w-fit rounded-md border px-4 text-sm">保存成员</button>
              <input value={testURL} onChange={(event) => setTestURL(event.target.value)} placeholder="测试环境地址" className="h-9 rounded-md border px-3 text-sm" />
              <button type="button" onClick={() => void saveEnvironment()} className="h-9 w-fit rounded-md border px-4 text-sm">保存测试环境</button>
            </>
          ) : null}
        </div>
      ) : null}
      {screen === "all" && mode === "chat" ? (
        <>
          <div className="flex flex-col gap-3">
            {items.map((item) => (
              <div id={`intake-${item.issue_id}`} key={item.issue_id} className="rounded-md border px-3 py-2 text-sm">
                <div>{item.identifier ? `${item.identifier} · ` : ""}{item.body}</div>
                <div className="text-[var(--text-secondary)]">{item.display_status}{retryHint(item.progress?.done, item.progress?.remain)}{item.run?.status === "running" && item.run.started_at ? ` · 已执行 ${elapsedSince(item.run.started_at, clock)}` : ""}</div>
                {item.start_at ? <div>开始：{item.start_at}</div> : null}
                {item.blocker?.done ? <div>已经做了：{item.blocker.done}</div> : null}
                {item.blocker?.missing ? <div>缺什么：{item.blocker.missing}</div> : null}
                {item.blocker?.consequence ? <div>不补会怎样：{item.blocker.consequence}</div> : null}
                {item.progress?.done ? <div>做到：{item.progress.done}</div> : null}
                {environmentLine(item.progress?.done, item.progress?.remain)}
                {item.result_summary ? <div>结果：{item.result_summary}</div> : null}
                {item.can_continue ? (
                  <div className="mt-2 flex flex-col gap-2">
                    <textarea value={note} onChange={(event) => setNote(event.target.value)} placeholder="补充一句" className="min-h-16 rounded-md border p-2" />
                    <div className="flex gap-2">
                      <button type="button" onClick={() => void decide(item.issue_id, "continue")} className="h-8 rounded-md bg-[var(--primary)] px-3 text-[var(--primary-foreground)]">继续</button>
                      <button type="button" onClick={() => void decide(item.issue_id, "stop")} className="h-8 rounded-md border px-3">停止</button>
                    </div>
                  </div>
                ) : null}
                {item.can_match ? (
                  <button type="button" onClick={() => void decide(item.issue_id, "match")} className="mt-2 h-8 rounded-md border px-3">重试</button>
                ) : null}
                {item.can_resume ? (
                  <button type="button" onClick={() => void decide(item.issue_id, "resume")} className="mt-2 h-8 rounded-md border px-3">继续</button>
                ) : null}
                {item.can_cancel ? (
                  <button type="button" onClick={() => void decide(item.issue_id, "cancel")} className="mt-2 h-8 rounded-md border px-3">取消</button>
                ) : null}
                {item.can_accept ? (
                  <button type="button" onClick={() => void decide(item.issue_id, "accept")} className="mt-2 h-8 rounded-md bg-[var(--primary)] px-3 text-[var(--primary-foreground)]">验收</button>
                ) : null}
              </div>
            ))}
          </div>
          <input type="datetime-local" value={startAt} onChange={(event) => setStartAt(event.target.value)} className="h-9 rounded-md border px-3 text-sm" />
          <textarea
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder="写下几句话"
            className="min-h-28 rounded-md border border-[var(--border)] bg-[var(--background)] p-3 text-sm"
          />
          <button
            type="button"
            disabled={sending || text.trim() === ""}
            onClick={() => void send()}
            className="h-9 w-fit rounded-md bg-[var(--primary)] px-4 text-sm text-[var(--primary-foreground)] disabled:opacity-50"
          >
            发送
          </button>
          {status === "没发出去" ? (
            <button type="button" onClick={() => void send()} className="h-9 w-fit rounded-md border px-4 text-sm">重试</button>
          ) : null}
        </>
      ) : null}
      </div>
      <style>{`@media (prefers-reduced-motion: no-preference) { .intake-mode { animation: intake-mode-in 200ms ease; } @keyframes intake-mode-in { from { opacity: 0; } to { opacity: 1; } } }`}</style>
      <div className="min-h-16 text-sm text-[var(--text-secondary)]">{status}</div>
    </div>
  );
}

function retryHint(done?: string, remain?: string) {
  if (done === "正在换注册单位") return " · 正在换注册单位";
  if (remain === "重试之后继续" || remain === "十五分钟后再分一次") return " · 正在重试";
  return "";
}

function environmentLine(done?: string, remain?: string) {
  if (done === "还没开始" || remain === "需要维护") return <div>还没开始，恢复后可以继续</div>;
  if (!remain) return null;
  if (remain === "还没有测试环境" || remain.startsWith("测试环境：")) return <div>{remain}</div>;
  return <div>还剩：{remain}</div>;
}

function shownElapsed(item: { elapsed?: string; run?: { status: string; started_at?: string } }) {
  if (item.run?.status === "running" && item.run.started_at) return elapsedSince(item.run.started_at);
  return item.elapsed ?? "";
}

function elapsedSince(startedAt: string, now = Date.now()) {
  const ms = now - new Date(startedAt).getTime();
  if (!Number.isFinite(ms) || ms < 60_000) return "不到 1 分钟";
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours} 小时` : `${hours} 小时 ${rest} 分钟`;
}

function ProjectRequests({
  items,
  onDecide,
  note,
  onNote,
}: {
  items: IntakeProjectItem[];
  onDecide: (issueId: string, action: "continue" | "stop" | "accept" | "cancel" | "resume" | "match") => void;
  note: string;
  onNote: (value: string) => void;
}) {
  const groups = new Map<string, IntakeProjectItem[]>();
  for (const item of items) {
    const title = item.project_title || "未命名项目";
    groups.set(title, [...(groups.get(title) ?? []), item]);
  }
  if (groups.size === 0) {
    return <div className="text-sm text-[var(--text-secondary)]">还没有所属项目的需求</div>;
  }
  return (
    <div className="flex flex-col gap-3">
      {[...groups.entries()].map(([title, rows]) => (
        <div key={title} className="flex flex-col gap-2">
          <div className="text-sm">{title}</div>
          {rows.map((item) => (
            <div key={item.issue_id} className="rounded-md border px-3 py-2 text-sm">
              <div>{item.identifier ? `${item.identifier} · ` : ""}{item.body}</div>
              {item.unit_switches ? <div>换过 {item.unit_switches} 次</div> : null}
              {item.unit_log ? <div className="whitespace-pre-line">用过：{item.unit_log}</div> : null}
              <div className="text-[var(--text-secondary)]">{item.display_status}{retryHint(item.progress?.done, item.progress?.remain)}{shownElapsed(item) ? ` · 已执行 ${shownElapsed(item)}` : ""}</div>
              {item.progress?.done ? <div>做到：{item.progress.done}</div> : null}
              {environmentLine(item.progress?.done, item.progress?.remain)}
              {item.result_summary ? <div>结果：{item.result_summary}</div> : null}
              {item.agent_name ? <div>执行：{item.agent_name}</div> : null}
              {item.blocker?.done ? <div>已经做了：{item.blocker.done}</div> : null}
              {item.blocker?.missing ? <div>缺什么：{item.blocker.missing}</div> : null}
              {item.blocker?.consequence ? <div>不补会怎样：{item.blocker.consequence}</div> : null}
              {item.can_continue ? (
                <div className="mt-2 flex flex-col gap-2">
                  <textarea value={note} onChange={(event) => onNote(event.target.value)} placeholder="补充一句" className="min-h-16 rounded-md border p-2" />
                  <div className="flex gap-2">
                    <button type="button" onClick={() => onDecide(item.issue_id, "continue")}>继续</button>
                    <button type="button" onClick={() => onDecide(item.issue_id, "stop")}>停止</button>
                  </div>
                </div>
              ) : null}
              {item.can_resume ? (
                <button type="button" onClick={() => onDecide(item.issue_id, "resume")}>继续执行</button>
              ) : null}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

function OwnAgent({
  agent,
  slug,
  onSave,
}: {
  agent: IntakeAgent;
  slug: string;
  onSave: (can: string, cannot: string) => Promise<void>;
}) {
  const [can, setCan] = useState(agent.can ?? "");
  const [cannot, setCannot] = useState(agent.cannot ?? "");
  const where = agent.runtime_mode === "cloud" ? "云上" : "本机";
  return (
    <div className="flex flex-col gap-2 rounded-md border px-3 py-2 text-sm">
      <div>{agent.name} · {where} · {agent.online ? "在线" : "不在线"} · 正在做 {agent.active_count ?? 0} 条</div>
      {(agent.placements ?? []).map((place) => (
        <div key={`${place.project}-${place.product}-${place.module}-${place.role}`}>{place.project} / {place.product} / {place.module} · {place.role}</div>
      ))}
      <input value={can} onChange={(event) => setCan(event.target.value)} placeholder="负责什么" className="h-9 rounded-md border px-3 text-sm" />
      <input value={cannot} onChange={(event) => setCannot(event.target.value)} placeholder="不接什么" className="h-9 rounded-md border px-3 text-sm" />
      <div className="flex gap-2">
        <button type="button" onClick={() => void onSave(can, cannot)} className="h-8 rounded-md border px-3">保存范围</button>
        {slug ? <AppLink href={`/${slug}/agents/${agent.id}`}>调试</AppLink> : null}
      </div>
    </div>
  );
}

function CapabilityMap({
  modules,
  catalog,
  onChange,
}: {
  modules: IntakeModule[];
  catalog: IntakeAgent[];
  onChange: (next: IntakeModule[]) => void;
}) {
  const groups = new Map<string, IntakeModule[]>();
  for (const module of modules) {
    groups.set(module.product, [...(groups.get(module.product) ?? []), module]);
  }
  if (groups.size === 0) return null;
  function lookup(id: string) {
    return catalog.find((agent) => agent.id === id);
  }
  return (
    <div className="flex flex-col gap-3">
      {[...groups.entries()].map(([product, rows]) => (
        <div key={product} className="flex flex-col gap-2">
          <div className="text-sm">{product}</div>
          {rows.map((module) => {
            const primary = lookup(module.agent_id);
            const backup = module.backup_agent_id ? lookup(module.backup_agent_id) : undefined;
            return (
              <div key={`${module.product}-${module.module}`} className="flex flex-col gap-2 rounded-md border px-3 py-2 text-sm">
                <div>{module.module} · {module.scope}</div>
                <div>主责 {primary?.name ?? "未指定"} · {primary?.online ? "在线" : "不在线"} · 正在做 {primary?.active_count ?? 0} 条</div>
                <div>备责 {backup?.name ?? "没有"}{backup ? ` · ${backup.online ? "在线" : "不在线"} · 正在做 ${backup.active_count ?? 0} 条` : ""}</div>
                <select
                  value={module.agent_id}
                  onChange={(event) => onChange(modules.map((row) => row.product === module.product && row.module === module.module ? { ...row, agent_id: event.target.value } : row))}
                  className="h-9 rounded-md border px-3 text-sm"
                >
                  {catalog.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
                </select>
                <select
                  value={module.backup_agent_id ?? ""}
                  onChange={(event) => onChange(modules.map((row) => row.product === module.product && row.module === module.module ? { ...row, backup_agent_id: event.target.value || undefined } : row))}
                  className="h-9 rounded-md border px-3 text-sm"
                >
                  <option value="">不设备责</option>
                  {catalog.filter((agent) => agent.id !== module.agent_id).map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
                </select>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
