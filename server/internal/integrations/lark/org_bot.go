package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OrgBotChannelType labels the deployment-scoped bot row in
// org_channel_installation. It is not a channel_installation channel type:
// that table requires one agent per row, while this bot serves every project.
const OrgBotChannelType = "feishu_org"

// OrgBot turns the deployment's Feishu login app into one organization-wide
// bot. It owns a single long connection and dispatches into the organization
// workspace, where departments and their agents work. Each /dispatch goes
// through the same OrgDispatchService as the in-app dialog, so department
// visibility, squad leadership and the requester's twin gate stay identical.
type OrgBot struct {
	cfg   OrgBotConfig
	files orgBotFileBuffer
	menu  orgBotMenuMemory
}

// OrgBotIdentityStore resolves a Feishu sender to a Multica user. It reads
// feishu_identity, the login identity the same app writes, rather than the
// per-installation channel_user_binding: the organization bot has no
// installation row to bind against.
type OrgBotIdentityStore interface {
	GetFeishuIdentityByOpenID(ctx context.Context, openID string) (db.FeishuIdentity, error)
}

// OrgBotOrganizationStore finds the organization workspace dispatches land in.
type OrgBotOrganizationStore interface {
	GetOrganizationWorkspace(ctx context.Context) (db.Workspace, error)
}

// OrgBotRouter is the workbench's natural-language routing. A private
// message uses it so the sender does not have to write a /dispatch command.
type OrgBotRouter interface {
	Route(ctx context.Context, wsUUID, actorID pgtype.UUID, text string, explicitUnitID pgtype.UUID) (service.RouteResult, error)
}

// OrgBotDedupStore drops replayed events after a reconnect. It reuses the
// channel dedup queries (and their claim-token fencing and stale-claim
// reclaim) keyed by the org installation id.
type OrgBotDedupStore interface {
	ClaimChannelInboundDedup(ctx context.Context, arg db.ClaimChannelInboundDedupParams) (db.ChannelInboundMessageDedup, error)
	MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error)
	ReleaseChannelInboundDedup(ctx context.Context, arg db.ReleaseChannelInboundDedupParams) (int64, error)
}

// OrgBotConfig wires the bot. Credentials are the login app's env credentials;
// they are never persisted in org_channel_installation.
type OrgBotConfig struct {
	InstallationID pgtype.UUID
	BotOpenID      string
	Credentials    InstallationCredentials

	Connector    EventConnector
	Client       APIClient
	Identity     OrgBotIdentityStore
	Organization OrgBotOrganizationStore
	Dedup        OrgBotDedupStore
	Dispatcher   engine.OrgDispatcher
	// Router turns a plain private message into the same department or
	// project choice the in-app workbench makes. Commands still use
	// Dispatcher directly.
	Router OrgBotRouter
	// Projects lists projects this person may hand work. Nil means the
	// picker only accepts a typed project name.
	Projects OrgBotProjects
	// Departments lists departments this person may hand work. Nil means the
	// picker still offers the personal squad and any projects.
	Departments OrgBotDepartments
	// Context remembers the target chosen in this chat. Nil means each
	// message is routed on its own.
	Context     OrgBotContextStore
	Chat        OrgBotChatStore
	Attachments OrgBotAttachmentStore

	AppURL string
	Logger *slog.Logger
}

// NewOrgBot validates the dependencies the bot cannot run without.
func NewOrgBot(cfg OrgBotConfig) (*OrgBot, error) {
	if cfg.Credentials.AppID == "" || cfg.Credentials.AppSecret == "" {
		return nil, errors.New("lark org bot: missing app credentials")
	}
	if cfg.Connector == nil || cfg.Client == nil {
		return nil, errors.New("lark org bot: missing transport")
	}
	if cfg.Identity == nil || cfg.Organization == nil || cfg.Dedup == nil {
		return nil, errors.New("lark org bot: missing store")
	}
	if cfg.Dispatcher == nil || cfg.Chat == nil {
		return nil, errors.New("lark org bot: missing dispatcher")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &OrgBot{cfg: cfg}, nil
}

// Run holds the long connection until ctx is cancelled or the link drops. The
// caller (the boot worker) owns lease fencing and reconnect backoff.
func (b *OrgBot) Run(ctx context.Context) error {
	inst := Installation{
		ID:        b.cfg.InstallationID,
		AppID:     b.cfg.Credentials.AppID,
		BotOpenID: b.cfg.BotOpenID,
		Region:    string(b.cfg.Credentials.Region),
	}
	return b.cfg.Connector.Run(ctx, inst, b.Handle)
}

// Handle processes one decoded event and sends at most one reply. A returned
// error is infrastructure-only and makes the connector NACK so Lark retries;
// business refusals are replies with a nil error.
func (b *OrgBot) Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
	if msg.MessageType == orgBotMenuActionType && msg.ChatID == "" {
		if chatID, ok := b.knownP2PChat(msg.SenderOpenID); ok {
			msg.ChatID = chatID
			msg.ChatType = ChatTypeP2P
			msg.AddressedToBot = true
		}
	}
	b.noteP2PChat(msg)
	// A menu click arrives without a chat id until this process has seen the
	// private chat. The selection is held and applied to the next message.
	menuBeforeChat := msg.MessageType == orgBotMenuActionType && msg.ChatID == "" && msg.SenderOpenID != "" && msg.MessageID != ""
	if !menuBeforeChat && (msg.ChatID == "" || msg.MessageID == "" || msg.SenderOpenID == "") {
		return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, nil
	}
	// Logging only p2p and addressed group messages keeps chatty groups quiet
	// while making a silent bot diagnosable: no "inbound message" line means
	// the event never reached this deployment.
	if msg.ChatType == ChatTypeP2P || msg.AddressedToBot {
		b.cfg.Logger.Info("lark org bot: inbound message",
			"message_id", msg.MessageID,
			"chat_id", string(msg.ChatID),
			"chat_type", string(msg.ChatType),
			"message_type", msg.MessageType,
		)
	}
	// Group chats only ingest messages that mention the bot; p2p is always
	// addressed. This mirrors the per-agent pipeline.
	if msg.ChatType == ChatTypeGroup && !msg.AddressedToBot {
		return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonNotAddressedInGroup}, nil
	}

	claim, err := b.cfg.Dedup.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: b.cfg.InstallationID,
		MessageID:      msg.MessageID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		b.cfg.Logger.Info("lark org bot: duplicate message dropped", "message_id", msg.MessageID)
		return DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonDuplicate}, nil
	}
	if err != nil {
		return DispatchResult{}, fmt.Errorf("claim org inbound message: %w", err)
	}

	// Build the reply (including the dispatch, which is the side effect) before
	// marking the message processed. An infrastructure failure here releases
	// the claim so Lark's retry can run it again.
	reply, result, err := b.reply(ctx, msg)
	release := func() {
		if _, releaseErr := b.cfg.Dedup.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
			InstallationID: b.cfg.InstallationID,
			MessageID:      msg.MessageID,
			ClaimToken:     claim.ClaimToken,
		}); releaseErr != nil {
			b.cfg.Logger.Warn("lark org bot: release inbound claim failed",
				"message_id", msg.MessageID, "error", releaseErr)
		}
	}
	if err != nil {
		release()
		return DispatchResult{}, err
	}

	// Mark first, then send: a lost reply must not replay the event and create
	// the work twice. The log line is the only trace of a dropped reply.
	if _, err := b.cfg.Dedup.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: b.cfg.InstallationID,
		MessageID:      msg.MessageID,
		ClaimToken:     claim.ClaimToken,
	}); err != nil {
		b.cfg.Logger.Warn("lark org bot: mark inbound message processed failed",
			"message_id", msg.MessageID, "error", err)
	}
	if reply != "" {
		var sendErr error
		switch {
		case msg.MessageType == orgBotChatEnteredType:
			sendErr = b.sendOpenGuide(ctx, msg, reply)
		case msg.ChatID == "":
			sendErr = b.sendPersonalText(ctx, msg, reply)
		default:
			sendErr = b.sendText(ctx, msg, reply)
		}
		if sendErr != nil {
			b.cfg.Logger.Warn("lark org bot: reply failed",
				"chat_id", string(msg.ChatID), "message_id", msg.MessageID, "error", sendErr)
		}
	}
	return result, nil
}

// reply decides what the message means and returns the text to send.
func (b *OrgBot) reply(ctx context.Context, msg InboundMessage) (string, DispatchResult, error) {
	if msg.MessageType == orgBotChatEnteredType {
		return b.refreshMenu(ctx, msg)
	}
	if msg.MessageType == orgBotMenuActionType {
		return b.workbenchReply(ctx, msg, strings.TrimSpace(msg.CommandBody))
	}
	// Media carries no text; its meaning comes from the thread it was sent in.
	if msg.MessageType == "image" || msg.MessageType == "file" {
		return b.mediaReply(ctx, msg)
	}
	command := strings.TrimSpace(msg.CommandBody)
	if command == "" {
		// Stickers, voice and video carry no dispatchable text. Offer the same
		// department, squad and project choices a person can tap on a real request.
		return b.workbenchReply(ctx, msg, "换目标")
	}
	if chatCommand, ok := parseOrgChatCommand(command); ok {
		return b.chatCommandReply(ctx, msg, chatCommand)
	}

	cmd, ok := engine.ParseDispatchCommand(command)
	if !ok {
		// A reply inside a dispatch topic continues that piece of work.
		// Anything else addressed to the bot — a private chat, or an @ in a
		// group — is a new request, the same as the in-app composer.
		if msg.ThreadID != "" {
			return b.followUpReply(ctx, msg, command)
		}
		return b.workbenchReply(ctx, msg, command)
	}
	if !cmd.Valid() {
		return orgBotUsageCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}

	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return b.loginPromptReply(), DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}

	org, err := b.cfg.Organization.GetOrganizationWorkspace(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return orgBotNoOrganizationCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	}
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("load organization workspace: %w", err)
	}

	matches, err := b.cfg.Dispatcher.ResolveUnits(ctx, org.ID, userID, cmd.Unit)
	if err != nil {
		if errors.Is(err, service.ErrDispatchRequesterNotInTeam) {
			return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
		}
		return "", DispatchResult{}, fmt.Errorf("resolve dispatch unit: %w", err)
	}
	switch {
	case len(matches) == 0:
		return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case len(matches) > 1:
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, match.Path)
		}
		return dispatchAmbiguousText(paths), DispatchResult{Outcome: OutcomeDispatchAmbiguous}, nil
	}

	result, err := b.cfg.Dispatcher.Dispatch(ctx, service.OrgDispatchParams{
		WorkspaceID: org.ID,
		ActorID:     userID,
		UnitID:      matches[0].Unit.ID,
		Title:       cmd.Title,
		Description: cmd.Description(),
		Origin: service.DispatchOriginInfo{
			ChannelType:    service.DispatchChannelFeishu,
			InstallationID: b.cfg.InstallationID,
			ChatID:         string(msg.ChatID),
			ThreadID:       msg.ThreadID,
			MessageID:      msg.MessageID,
		},
		Platform: string(channel.TypeFeishu),
	})
	switch {
	case errors.Is(err, service.ErrDispatchRequesterUnbound):
		return dispatchUnboundCopy, DispatchResult{Outcome: OutcomeDispatchUnbound}, nil
	case errors.Is(err, service.ErrOrgUnitNotFound),
		errors.Is(err, service.ErrOrgUnitNoAccess),
		errors.Is(err, service.ErrDispatchRequesterNotInTeam):
		return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrOrgUnitNoAgents):
		return dispatchNoAgentsCopy, DispatchResult{Outcome: OutcomeDispatchNoAgents}, nil
	case errors.Is(err, service.ErrDispatchLeaderUnavailable):
		return dispatchLeaderUnavailableCopy, DispatchResult{Outcome: OutcomeDispatchLeaderUnavailable}, nil
	case errors.Is(err, service.ErrDispatchLeaderOffline):
		return dispatchLeaderOfflineCopy, DispatchResult{Outcome: OutcomeDispatchLeaderOffline}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("dispatch to org unit: %w", err)
	}

	identifier, slug := dispatchedIssueRef(org, result)
	note := b.attachStagedFiles(ctx, msg, userID, org.ID, result.Issue.ID)
	reply := b.presentDispatched(ctx, msg, matches[0].Path, result.Issue.Title, identifier, matches[0].Path, slug, note)
	b.saveTarget(ctx, msg, userID, OrgBotChatContext{
		Kind:                orgBotTargetDepartment,
		UnitID:              matches[0].Unit.ID,
		UnitPath:            matches[0].Path,
		LastIssueID:         result.Issue.ID,
		LastIssueIdentifier: identifier,
	})
	return reply, DispatchResult{
		Outcome:            OutcomeDispatchCreated,
		IssueID:            result.Issue.ID,
		IssueNumber:        result.Issue.Number,
		IssueIdentifier:    identifier,
		IssueWorkspaceSlug: slug,
		IssueTitle:         result.Issue.Title,
		DispatchUnitName:   result.Unit.Name,
	}, nil
}

// senderUser resolves the Feishu sender. unbound is true when the sender has
// no login identity; infrastructure failures come back as errors so the
// connector retries instead of telling a real member they have no account.
func (b *OrgBot) senderUser(ctx context.Context, msg InboundMessage) (pgtype.UUID, bool, error) {
	identity, err := b.cfg.Identity.GetFeishuIdentityByOpenID(ctx, string(msg.SenderOpenID))
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, true, nil
	}
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("resolve sender identity: %w", err)
	}
	return identity.UserID, false, nil
}

func (b *OrgBot) loginPromptReply() string {
	copy := orgBotLoginRequiredCopy
	if base := strings.TrimRight(b.cfg.AppURL, "/"); base != "" {
		copy += "\n" + base + "/login"
	}
	return copy
}

// orgBotOpenPost is the workbench empty state as one rich-text message:
// a title, the italic explanation, and the example list.
func orgBotOpenPost() (string, error) {
	doc := map[string]any{
		"zh_cn": map[string]any{
			"title": "告诉团队要做什么",
			"content": []any{
				[]any{map[string]any{
					"tag":   "text",
					"text":  "用一句话描述要做的事，系统会判断交给哪个部门，部门负责人再拆分成任务。",
					"style": []string{"italic"},
				}},
				[]any{map[string]any{
					"tag":  "md",
					"text": "可以这样说\n- 准备下周的客户上线材料\n- 排查昨天结算失败的原因\n- 给新看板写一篇发布公告",
				}},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (b *OrgBot) sendOpenGuide(ctx context.Context, msg InboundMessage, content string) error {
	sender, ok := b.cfg.Client.(interface {
		SendPostMessage(context.Context, SendPostParams) (string, error)
	})
	if !ok {
		return errors.New("lark client cannot send a post")
	}
	_, err := sender.SendPostMessage(ctx, SendPostParams{
		InstallationID: b.cfg.Credentials,
		ChatID:         msg.ChatID,
		Content:        content,
	})
	return err
}

func (b *OrgBot) sendText(ctx context.Context, msg InboundMessage, text string) error {
	return sendWithReplyFallback(b.cfg.Logger, "send org bot reply", inboundReplyTarget(msg), func(target ReplyTarget) error {
		_, err := b.cfg.Client.SendTextMessage(ctx, SendTextParams{
			InstallationID: b.cfg.Credentials,
			ChatID:         msg.ChatID,
			Text:           text,
			ReplyTarget:    target,
		})
		return err
	})
}

const (
	// The title must precede the first section header: the parser treats every
	// unstructured line after a header as that section's body.
	orgBotUsageCopy = "我是 Multica 组织 Bot，可以帮你给部门派活。\n\n" +
		"`/dispatch <部门名或完整路径>`\n" +
		"`<标题>`\n" +
		"`目标: ...`\n" +
		"`验收: ...`\n\n" +
		"`输入: ...` 和 `边界: ...` 为可选项。\n\n" +
		"在派活的话题里直接回复可以追问或附文件。\n" +
		"`/status` 查看进行中的派活；`/cancel 编号` 取消；`/reassign 编号 <部门>` 改派；" +
		"`/accept 编号` 验收通过；`/reject 编号 <原因>` 驳回返工。"
	orgBotLoginRequiredCopy  = "还没有找到你的 Multica 账户。请先用飞书登录一次 Multica，再回来派活："
	orgBotNoOrganizationCopy = "组织工作区还没有建立，暂时不能派活。请联系管理员。"
	orgBotP2PHintCopy        = "直接把要做的事发给我，和在易研里发一样。例如：帮支付组看一下对账差异。回复「换目标」可以选择部门、我的小队或项目。"
	orgBotP2PAmbiguousCopy   = "有多个部门可以接。回复序号或名称，你的原话会交给它。也可以回复「我的小队」或「项目」。"
	orgBotP2PTooLongCopy     = "这条消息太长了，请缩短后再发。"
)
