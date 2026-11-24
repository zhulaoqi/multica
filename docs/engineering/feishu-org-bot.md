# 飞书组织机器人：运行方式与排查

组织机器人（`OrgBot`）把飞书登录应用当成全公司唯一的派活入口。它只连一条长连接，把 `/dispatch` 落到组织工作区，部门负责人在那里拆解并派给组内 Agent。代码在 `server/internal/integrations/lark/org_bot*.go`，启动和租约在 `server/cmd/server/feishu_orgbot.go`。

这份文档给运维和值班用：机器人不回消息时，按下面的顺序查。

## 运行前提

| 项 | 说明 |
| --- | --- |
| `MULTICA_FEISHU_APP_ID` / `MULTICA_FEISHU_APP_SECRET` | 就是网页飞书登录用的那套应用凭据。缺一个，机器人不启动。 |
| 机器人能力 | 飞书开放平台里给这个应用开启「机器人」。 |
| 事件订阅 | 订阅方式必须是**长连接**，并添加 `im.message.receive_v1`。输入框上方的个人菜单还要订阅 `application.bot.menu_v6` 和 `im.chat.access_event.bot_p2p_chat_entered_v1`。卡片按钮还要订阅 `card.action.trigger`。 |
| 权限 | 至少 `im:message`、`im:message:send_as_bot`。个人菜单还要 `application:bot.menu:write`。 |
| 版本 | 应用版本发布并通过审核，企业内可见。 |
| 单连接 | 一个飞书应用只能有一条长连接。同一个 app id 还被某个工作区的单 Agent 机器人占用时，组织机器人会主动拒绝启动。 |
| 单实例 | 多副本部署时靠 `org_channel_installation` 的租约选出一个副本连长连接，其余副本空转。 |

## 启动日志对照

启动日志里搜 `feishu org bot`，每条都对应一个明确状态：

| 日志 | 含义 |
| --- | --- |
| `feishu org bot enabled app_id=… bot_open_id=…` | 正常启动，已拿到机器人身份并写入组织安装行。 |
| `feishu org bot disabled (login app not configured)` | 没配 `MULTICA_FEISHU_APP_ID` / `MULTICA_FEISHU_APP_SECRET`。 |
| `feishu org bot disabled (dispatch service unavailable)` | 派活服务没初始化，通常是数据库或依赖启动失败，先看更早的错误。 |
| `feishu org bot: registration failed; retrying` | 拿不到机器人身份，或应用已被单 Agent 机器人占用。日志里的 `error` 原文决定下一步：权限/能力没开、事件订阅方式不对，或 `already bound to a per-agent bot installation`。 |
| `feishu org bot: connection ended` | 长连接被断开，之后按退避重连。偶发可以忽略，持续出现要查网络和飞书侧状态。 |
| `feishu org bot: acquire lease failed` | 数据库租约拿不到，通常是数据库不可用或另一个副本持锁。 |

## 收到消息之后的日志

| 日志 | 含义 |
| --- | --- |
| `lark org bot: inbound message message_id=… chat_type=… message_type=…` | 事件已经到达本部署。只记 p2p 和被 @ 的群消息，群里没 @ 机器人的消息不记，避免刷屏。 |
| `lark org bot: duplicate message dropped` | 飞书重投的同一条消息被去重丢掉。 |
| `lark org bot: reply failed` | 事件处理成功但回复发送失败。回复不会重放（见下），这一行就是用户侧静默的原因。 |

**私聊发消息没有回复，先看有没有 `inbound message`。** 没有这一行，说明事件没到本部署，问题在飞书侧或连接侧；有这一行但没有回复，看同一 `message_id` 后面的 `reply failed`。

## 静默排查顺序

1. 启动日志是 `enabled` 吗？不是就按上表修配置或能力。
2. 飞书开放平台确认事件订阅方式是长连接、已订阅 `im.message.receive_v1`（菜单还要 `application.bot.menu_v6` 和 `im.chat.access_event.bot_p2p_chat_entered_v1`）、权限齐全、版本已发布。私聊的必须是这个应用的机器人，不是别的同名机器人。
3. 查 `channel_installation` 里是否还有 `channel_type='feishu'` 且 `app_id` 等于登录应用的安装行。有就说明单 Agent 机器人和组织机器人抢同一个应用；先解绑前者再重启。
4. 多副本时确认租约持有者不是升级前的旧 Pod：旧镜像没有组织机器人，却可能仍持着租约，新副本因此不连接。重启一次 deployment，或临时缩到 1 个副本验证。
5. 搜 `lark org bot: reply failed` 看发送侧错误（缺 `im:message:send_as_bot`、机器人被移出会话、限流等）。

## 行为约定

- 私聊文字一定会得到回复：未绑定账户回登录提示；没有选定目标时先给出部门、小队和项目；选定之后文字就是需求。
- 单聊输入框上方是这个人自己可见的菜单：我的小队、部门、项目。点一下确定目标，下一条消息交给它。超过 10 个的部门或项目，最后一项是「更多」，打开完整卡片。
- 群聊仍用对话选择小队、部门和项目。选定之后，下一条 @ 是需求。要更换，必须发送「换项目」「换部门」或「换小队」。
- 表情包、语音、视频这类没有文字的消息回一条用法提示，不再静默丢弃。
- 图片和文件在派活话题里作为附件落到对应任务；没有对应派活时回提示。
- 处理顺序是「先标记已处理，再发送回复」。发送失败只留 `reply failed` 日志，不重放事件，避免一次派活被执行两遍。
