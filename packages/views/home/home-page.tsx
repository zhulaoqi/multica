"use client";

import { useEffect, useState } from "react";
import { ListTodo, MessagesSquare, type IconComponent } from "@multica/ui/icons";
import { HomeModeProvider, cendDispatchPath } from "../common/home-mode";
import { useT } from "../i18n";
import { AccountMenu } from "../layout/account-menu";
import { useNavigation } from "../navigation";
import { WorkbenchPage } from "../workbench";
import { HomeTasksPage } from "./home-tasks-page";

type HomeTab = "conversation" | "tasks";

const TAB_CLASS =
  "group relative flex h-[52px] w-[52px] flex-col items-center justify-center gap-1 rounded-xl text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-ring aria-[current=page]:bg-sidebar-accent aria-[current=page]:font-medium aria-[current=page]:text-sidebar-accent-foreground";

/**
 * The C-end: everyone works here, in the organization, without the workspace
 * chrome. The conversation is the workbench; tasks are the requests that came
 * out of it. The account menu at the foot opens the admin side, where the
 * organization, its projects and its agents are managed.
 */
export function HomePage() {
  const { t } = useT("workbench");
  const { push, searchParams } = useNavigation();
  const [tab, setTab] = useState<HomeTab>("conversation");
  // A dispatch link (notification click, shared URL) always opens its thread.
  const dispatchId = searchParams.get("dispatch");
  useEffect(() => {
    if (dispatchId) setTab("conversation");
  }, [dispatchId]);
  const tabs: { id: HomeTab; icon: IconComponent; label: string }[] = [
    { id: "conversation", icon: MessagesSquare, label: t(($) => $.home.conversation) },
    { id: "tasks", icon: ListTodo, label: t(($) => $.home.tasks) },
  ];

  return (
    <HomeModeProvider>
      <div className="flex h-svh bg-app-shell">
        <nav
          aria-label={t(($) => $.home.label)}
          className="flex w-16 shrink-0 flex-col items-center overflow-y-auto overflow-x-hidden pt-3 pb-2.5 [scrollbar-width:none]"
        >
          <ul className="flex flex-col items-center gap-0.5">
            {tabs.map(({ id, icon: Icon, label }) => (
              <li key={id}>
                <button
                  type="button"
                  data-space={id}
                  aria-current={tab === id ? "page" : undefined}
                  onClick={() => setTab(id)}
                  className={TAB_CLASS}
                >
                  <Icon className="size-5" />
                  <span className="max-w-full truncate px-1 text-micro leading-[14px]">{label}</span>
                </button>
              </li>
            ))}
          </ul>
          <div className="mt-auto flex flex-col items-center gap-1 pt-2">
            <AccountMenu variant="home" />
          </div>
        </nav>
        <main className="relative flex min-w-0 flex-1 flex-row overflow-hidden bg-page-canvas md:my-2 md:mr-2 md:rounded-xl md:shadow-[var(--surface-shadow)] md:ring-1 md:ring-surface-border">
          <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
            {tab === "conversation" ? (
              <WorkbenchPage />
            ) : (
              <HomeTasksPage
                onOpen={(issueId) => {
                  setTab("conversation");
                  push(cendDispatchPath(issueId));
                }}
              />
            )}
          </div>
        </main>
      </div>
    </HomeModeProvider>
  );
}
