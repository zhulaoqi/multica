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
    <>
      <div className="flex items-center gap-3">
        <button type="button" className="text-sm font-medium" onClick={onHelp}>收口</button>
        <button type="button" className="ml-auto text-sm" onClick={onMenu}>{person || "我"}</button>
        {showMode ? <button type="button" className="w-fit text-sm" onClick={onMode}>{mode === "chat" ? "管理" : "聊天"}</button> : null}
      </div>
      {menuOpen ? (
        <div className="self-end rounded-md border p-3 text-sm">
          <div>{person || "当前登录的人"}</div>
          {workspaceName ? <div>{workspaceName}</div> : null}
          <button type="button" className="mt-2 underline" onClick={onSignOut}>退出</button>
        </div>
      ) : null}
    </>
  );
}
