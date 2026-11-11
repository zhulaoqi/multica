export function HelpPanel({ canMaintain, onClose }: { canMaintain: boolean; onClose: () => void }) {
  return (
    <div className="rounded-md border p-3 text-sm">
      <p>写下几句话就发送。系统自己接过去，在同一个分支上做完，只部署一次。</p>
      <p>你只看做到哪、还剩什么。卡住时先通知你，15 分钟没有决定就按已有信息继续。定时在个人管理里看。</p>
      {canMaintain ? <p>能力总览按产品列出模块。在这里换主责、备责、成员和测试环境。</p> : null}
      <button type="button" className="mt-2 underline" onClick={onClose}>关闭</button>
    </div>
  );
}
