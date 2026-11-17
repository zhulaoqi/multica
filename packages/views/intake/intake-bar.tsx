export function IntakeBar({
  person,
  workspaceName,
  mode,
  menuOpen,
  onHelp,
  onMenu,
  onMode,
  onSignOut,
  showMode = true,
}: {
  person: string;
  workspaceName?: string;
  mode: "chat" | "manage";
  menuOpen: boolean;
  onHelp: () => void;
  onMenu: () => void;
  onMode: () => void;
  onSignOut: () => void;
  showMode?: boolean;
}) {
  return (
    <div className="flex items-center gap-3">
      <button type="button" className="text-sm font-medium" onClick={onHelp}>收口</button>
      <div className="ml-auto flex items-center gap-3">
        <div className="relative">
          <button
            type="button"
            className="rounded-md px-2 py-1 text-sm hover:bg-muted"
            aria-expanded={menuOpen}
            onClick={onMenu}
          >
            {person || "我"}
          </button>
          {menuOpen ? (
            <div className="absolute right-0 top-full z-50 mt-2 w-48 rounded-md border bg-popover p-3 text-sm text-popover-foreground shadow-md">
              <div>{person || "当前登录的人"}</div>
              {workspaceName ? <div>{workspaceName}</div> : null}
              <button type="button" className="mt-2 underline" onClick={onSignOut}>退出</button>
            </div>
          ) : null}
        </div>
        {showMode ? <button type="button" className="w-fit text-sm" onClick={onMode}>{mode === "chat" ? "管理" : "聊天"}</button> : null}
      </div>
    </div>
  );
}
