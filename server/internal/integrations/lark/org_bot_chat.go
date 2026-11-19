package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The org bot is also the conversation the requester manages their dispatches
// from. These commands stay requester-scoped in the service layer: whoever
// asked for the work is the one who can inspect, cancel, accept or redirect it.

// OrgBotChatStore is the requester-scoped dispatch control surface.
type OrgBotChatStore interface {
	ListChatDispatches(ctx context.Context, requesterID pgtype.UUID, limit int32) ([]service.ChatDispatch, error)
	FindChatDispatch(ctx context.Context, requesterID pgtype.UUID, reference string) (service.ChatDispatch, error)
	GetChatDispatchByThread(ctx context.Context, requesterID pgtype.UUID, chatID, threadID string) (service.ChatDispatch, error)
	CancelChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID) (db.Issue, error)
	AcceptChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID) (db.Issue, error)
	RejectChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID, reason string) (service.ChatDispatchReply, error)
	FollowUpChatDispatch(ctx context.Context, actorID, issueID pgtype.UUID, text string) (service.ChatDispatchReply, error)
	ResolveUnits(ctx context.Context, wsUUID, actorID pgtype.UUID, query string) ([]service.UnitMatch, error)
	Reassign(ctx context.Context, p service.OrgDispatchReassignParams) (service.OrgDispatchResult, error)
}

// OrgBotAttachment is one media file the requester sent into a dispatch thread.
type OrgBotAttachment struct {
	ActorID     pgtype.UUID
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	Filename    string
	ContentType string
	Data        []byte
}

// OrgBotAttachmentResult reports what was stored so the reply can name it.
type OrgBotAttachmentResult struct {
	Filename  string
	SizeBytes int64
}

// OrgBotAttachmentStore persists an inbound file on the dispatched issue.
type OrgBotAttachmentStore interface {
	AttachToIssue(ctx context.Context, attachment OrgBotAttachment) (OrgBotAttachmentResult, error)
}

// orgBotAttachmentMaxBytes mirrors the attachment limit the API enforces.
const orgBotAttachmentMaxBytes = 30 << 20

type orgChatCommand struct {
	name string
	args string
}

// parseOrgChatCommand recognizes the dispatch control commands. Commands that
// mean "create work" are left to engine.ParseDispatchCommand.
func parseOrgChatCommand(command string) (orgChatCommand, bool) {
	line := strings.TrimSpace(strings.SplitN(command, "\n", 2)[0])
	fields := strings.Fields(line)
	index := -1
	for i, field := range fields {
		if strings.HasPrefix(field, "/") {
			index = i
			break
		}
	}
	if index < 0 {
		return orgChatCommand{}, false
	}
	var name string
	switch strings.ToLower(strings.TrimPrefix(fields[index], "/")) {
	case "status", "状态":
		name = "status"
	case "cancel", "取消":
		name = "cancel"
	case "reassign", "改派":
		name = "reassign"
	case "accept", "验收":
		name = "accept"
	case "reject", "驳回":
		name = "reject"
	default:
		return orgChatCommand{}, false
	}
	return orgChatCommand{name: name, args: strings.TrimSpace(strings.Join(fields[index+1:], " "))}, true
}

func (b *OrgBot) chatCommandReply(ctx context.Context, msg InboundMessage, command orgChatCommand) (string, DispatchResult, error) {
	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return b.loginPromptReply(), DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}
	if command.name == "status" {
		return b.statusReply(ctx, userID)
	}

	reference, rest := splitOrgCommandArg(command.args)
	dispatch, err := b.cfg.Chat.FindChatDispatch(ctx, userID, reference)
	if err != nil {
		return b.chatLookupErrorReply(ctx, userID, err)
	}

	switch command.name {
	case "cancel":
		return b.cancelReply(ctx, userID, dispatch)
	case "accept":
		return b.acceptReply(ctx, userID, dispatch)
	case "reject":
		return b.rejectReply(ctx, userID, dispatch, rest)
	case "reassign":
		return b.reassignReply(ctx, userID, dispatch, rest)
	}
	return orgBotUsageCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
}

func (b *OrgBot) statusReply(ctx context.Context, userID pgtype.UUID) (string, DispatchResult, error) {
	dispatches, err := b.cfg.Chat.ListChatDispatches(ctx, userID, 10)
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("list chat dispatches: %w", err)
	}
	if len(dispatches) == 0 {
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeIngested}, nil
	}
	return formatChatDispatches(dispatches, b.cfg.AppURL), DispatchResult{Outcome: OutcomeIngested}, nil
}

func (b *OrgBot) cancelReply(ctx context.Context, userID pgtype.UUID, dispatch service.ChatDispatch) (string, DispatchResult, error) {
	_, err := b.cfg.Chat.CancelChatDispatch(ctx, userID, dispatch.IssueID)
	switch {
	case errors.Is(err, service.ErrChatDispatchClosed):
		return fmt.Sprintf("%s 已经结束，不需要再取消。", dispatch.Identifier),
			DispatchResult{Outcome: OutcomeIngested}, nil
	case errors.Is(err, service.ErrChatDispatchNotFound):
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrChatDispatchNotOwner):
		return orgBotNotOwnerCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("cancel chat dispatch: %w", err)
	}
	return fmt.Sprintf("已取消 %s，相关任务已停止。", dispatch.Identifier),
		DispatchResult{Outcome: OutcomeIngested}, nil
}

func (b *OrgBot) acceptReply(ctx context.Context, userID pgtype.UUID, dispatch service.ChatDispatch) (string, DispatchResult, error) {
	_, err := b.cfg.Chat.AcceptChatDispatch(ctx, userID, dispatch.IssueID)
	switch {
	case errors.Is(err, service.ErrChatDispatchClosed):
		return fmt.Sprintf("%s 已经结束，不需要再验收。", dispatch.Identifier),
			DispatchResult{Outcome: OutcomeIngested}, nil
	case errors.Is(err, service.ErrChatDispatchNotFound):
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrChatDispatchNotOwner):
		return orgBotNotOwnerCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("accept chat dispatch: %w", err)
	}
	return fmt.Sprintf("已验收 %s，这项工作已结束。", dispatch.Identifier),
		DispatchResult{Outcome: OutcomeIngested}, nil
}

func (b *OrgBot) rejectReply(ctx context.Context, userID pgtype.UUID, dispatch service.ChatDispatch, reason string) (string, DispatchResult, error) {
	reply, err := b.cfg.Chat.RejectChatDispatch(ctx, userID, dispatch.IssueID, reason)
	switch {
	case errors.Is(err, service.ErrChatDispatchClosed):
		return fmt.Sprintf("%s 已经结束，不能再驳回。", dispatch.Identifier),
			DispatchResult{Outcome: OutcomeIngested}, nil
	case errors.Is(err, service.ErrChatDispatchNotFound):
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrChatDispatchNotOwner):
		return orgBotNotOwnerCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("reject chat dispatch: %w", err)
	}
	text := fmt.Sprintf("已退回 %s，队长会按你的意见重做。", dispatch.Identifier)
	if !reply.Delivered {
		text += orgBotLeaderOfflineHintCopy
	}
	return text, DispatchResult{Outcome: OutcomeIngested}, nil
}

func (b *OrgBot) reassignReply(ctx context.Context, userID pgtype.UUID, dispatch service.ChatDispatch, query string) (string, DispatchResult, error) {
	if strings.TrimSpace(query) == "" {
		return "请说明改派到哪个部门，例如 `/reassign 1 支付组`。",
			DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}
	matches, err := b.cfg.Chat.ResolveUnits(ctx, dispatch.WorkspaceID, userID, query)
	if err != nil {
		if errors.Is(err, service.ErrDispatchRequesterNotInTeam) {
			return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
		}
		return "", DispatchResult{}, fmt.Errorf("resolve reassign unit: %w", err)
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

	result, err := b.cfg.Chat.Reassign(ctx, service.OrgDispatchReassignParams{
		WorkspaceID: dispatch.WorkspaceID,
		ActorID:     userID,
		IssueID:     dispatch.IssueID,
		UnitID:      matches[0].Unit.ID,
	})
	switch {
	case errors.Is(err, service.ErrDispatchRequesterNotInTeam),
		errors.Is(err, service.ErrOrgUnitNotFound),
		errors.Is(err, service.ErrOrgUnitNoAccess):
		return dispatchNoAccessCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrOrgUnitNoAgents):
		return dispatchNoAgentsCopy, DispatchResult{Outcome: OutcomeDispatchNoAgents}, nil
	case errors.Is(err, service.ErrDispatchLeaderOffline):
		return dispatchLeaderOfflineCopy, DispatchResult{Outcome: OutcomeDispatchLeaderOffline}, nil
	case errors.Is(err, service.ErrDispatchLeaderUnavailable):
		return dispatchLeaderUnavailableCopy, DispatchResult{Outcome: OutcomeDispatchLeaderUnavailable}, nil
	case errors.Is(err, service.ErrDispatchReassignForbidden):
		return orgBotNotOwnerCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrDispatchNotFound):
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("reassign chat dispatch: %w", err)
	}
	return fmt.Sprintf("已改派 %s 给 %s，新队长会接手这项工作。", dispatch.Identifier, matches[0].Path),
		DispatchResult{
			Outcome:            OutcomeDispatchCreated,
			IssueID:            result.Issue.ID,
			IssueNumber:        result.Issue.Number,
			IssueIdentifier:    dispatch.Identifier,
			IssueWorkspaceSlug: dispatch.WorkspaceSlug,
			IssueTitle:         result.Issue.Title,
			DispatchUnitName:   result.Unit.Name,
		}, nil
}

// followUpReply turns an in-thread non-command message into a comment on the
// dispatch that owns the thread.
func (b *OrgBot) followUpReply(ctx context.Context, msg InboundMessage, text string) (string, DispatchResult, error) {
	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return b.loginPromptReply(), DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}
	dispatch, err := b.threadDispatch(ctx, userID, msg)
	if err != nil {
		return orgBotNoThreadCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}
	reply, err := b.cfg.Chat.FollowUpChatDispatch(ctx, userID, dispatch.IssueID, text)
	switch {
	case errors.Is(err, service.ErrChatDispatchClosed):
		return fmt.Sprintf("%s 已经结束，不能再追问。", dispatch.Identifier),
			DispatchResult{Outcome: OutcomeIngested}, nil
	case errors.Is(err, service.ErrChatDispatchNotFound):
		return orgBotNoThreadCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	case errors.Is(err, service.ErrChatDispatchNotOwner):
		return orgBotNotOwnerCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case err != nil:
		return "", DispatchResult{}, fmt.Errorf("follow up chat dispatch: %w", err)
	}
	text = fmt.Sprintf("已把你的追问转给 %s：%s。", chatDispatchTarget(dispatch), dispatch.Identifier)
	if !reply.Delivered {
		text += orgBotLeaderOfflineHintCopy
	}
	return text, DispatchResult{Outcome: OutcomeIngested}, nil
}

// mediaReply attaches an image or file from the conversation to the dispatch
// the thread belongs to.
func (b *OrgBot) mediaReply(ctx context.Context, msg InboundMessage) (string, DispatchResult, error) {
	if b.cfg.Attachments == nil {
		return orgBotNoAttachmentCopy, DispatchResult{Outcome: OutcomeIngested}, nil
	}
	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return b.loginPromptReply(), DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}
	dispatch, err := b.threadDispatch(ctx, userID, msg)
	if err != nil {
		return orgBotNoThreadCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}
	fileKey, filename, ok := orgMediaResource(msg)
	if !ok {
		return orgBotMediaUnsupportedCopy, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}

	resource, err := b.cfg.Client.DownloadMessageResource(ctx, b.cfg.Credentials, DownloadResourceParams{
		MessageID: msg.MessageID,
		FileKey:   fileKey,
		Type:      msg.MessageType,
	})
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("download inbound media: %w", err)
	}
	if int64(len(resource.Data)) > orgBotAttachmentMaxBytes {
		return orgBotAttachmentTooLargeCopy, DispatchResult{Outcome: OutcomeIngested}, nil
	}
	if name := strings.TrimSpace(resource.Filename); name != "" {
		filename = name
	}
	if filename == "" {
		filename = "attachment"
	}

	stored, err := b.cfg.Attachments.AttachToIssue(ctx, OrgBotAttachment{
		ActorID:     userID,
		WorkspaceID: dispatch.WorkspaceID,
		IssueID:     dispatch.IssueID,
		Filename:    filename,
		ContentType: resource.ContentType,
		Data:        resource.Data,
	})
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("attach inbound media: %w", err)
	}
	return fmt.Sprintf("已把 %s 附加到 %s。", stored.Filename, dispatch.Identifier),
		DispatchResult{Outcome: OutcomeIngested}, nil
}

func (b *OrgBot) threadDispatch(ctx context.Context, userID pgtype.UUID, msg InboundMessage) (service.ChatDispatch, error) {
	if msg.ThreadID != "" {
		return b.cfg.Chat.GetChatDispatchByThread(ctx, userID, string(msg.ChatID), msg.ThreadID)
	}
	if msg.ChatType == ChatTypeP2P {
		return b.cfg.Chat.FindChatDispatch(ctx, userID, "")
	}
	return service.ChatDispatch{}, service.ErrChatDispatchNotFound
}

func (b *OrgBot) chatLookupErrorReply(ctx context.Context, userID pgtype.UUID, err error) (string, DispatchResult, error) {
	switch {
	case errors.Is(err, service.ErrChatDispatchNotFound):
		return orgBotNoOpenDispatchCopy, DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	case errors.Is(err, service.ErrChatDispatchAmbiguous):
		dispatches, listErr := b.cfg.Chat.ListChatDispatches(ctx, userID, 10)
		if listErr != nil {
			return "", DispatchResult{}, fmt.Errorf("list chat dispatches: %w", listErr)
		}
		return orgBotAmbiguousDispatchCopy + formatChatDispatches(dispatches, b.cfg.AppURL),
			DispatchResult{Outcome: OutcomeDispatchAmbiguous}, nil
	default:
		return "", DispatchResult{}, fmt.Errorf("resolve chat dispatch: %w", err)
	}
}

func splitOrgCommandArg(args string) (string, string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	if i := strings.IndexAny(args, " \t"); i >= 0 {
		return args[:i], strings.TrimSpace(args[i+1:])
	}
	return args, ""
}

func orgMediaResource(msg InboundMessage) (fileKey, filename string, ok bool) {
	if msg.MessageType != "image" && msg.MessageType != "file" {
		return "", "", false
	}
	var content struct {
		ImageKey string `json:"image_key"`
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
		return "", "", false
	}
	if msg.MessageType == "image" {
		if content.ImageKey == "" {
			return "", "", false
		}
		return content.ImageKey, "image.png", true
	}
	if content.FileKey == "" {
		return "", "", false
	}
	return content.FileKey, content.FileName, true
}

func formatChatDispatches(dispatches []service.ChatDispatch, appURL string) string {
	var b strings.Builder
	b.WriteString("进行中的派活：")
	for i, dispatch := range dispatches {
		fmt.Fprintf(&b, "\n%d. %s — %s（%s · %s）",
			i+1, dispatch.Identifier, dispatch.Title, chatDispatchTarget(dispatch), chatDispatchState(dispatch))
		if link := dispatchLink(appURL, dispatch); link != "" {
			b.WriteString("\n   " + link)
		}
	}
	b.WriteString("\n\n`/cancel 编号` 取消 · `/reassign 编号 部门` 改派 · `/accept 编号` 验收 · `/reject 编号 原因` 驳回")
	return b.String()
}

func dispatchLink(appURL string, dispatch service.ChatDispatch) string {
	return channel.IssueWebLink(appURL, dispatch.WorkspaceSlug, dispatch.Identifier)
}

func chatDispatchTarget(dispatch service.ChatDispatch) string {
	if dispatch.Personal {
		return "个人小队"
	}
	if dispatch.UnitName != "" {
		return dispatch.UnitName
	}
	return "部门"
}

func chatDispatchState(dispatch service.ChatDispatch) string {
	switch {
	case dispatch.StatusCategory == "unstarted":
		return "待开始"
	case dispatch.LeaderState == "failed":
		return "队长执行失败"
	case dispatch.StatusCategory == "started":
		return "进行中"
	default:
		return dispatch.Status
	}
}

const (
	orgBotNoOpenDispatchCopy     = "你目前没有进行中的派活。"
	orgBotNoThreadCopy           = "没有找到这条对话对应的派活。请在派活的话题里追问，或用 `/status` 查看编号后带上编号。"
	orgBotAmbiguousDispatchCopy  = "匹配到多个派活，请用编号指定：\n"
	orgBotNotOwnerCopy           = "只有发起这项派活的人可以操作。"
	orgBotNoAttachmentCopy       = "当前没有启用附件处理，请把文件内容直接发在追问里。"
	orgBotMediaUnsupportedCopy   = "这条消息里没有可保存的附件。"
	orgBotAttachmentTooLargeCopy = "附件超过 30 MB，请压缩后重发。"
	orgBotLeaderOfflineHintCopy  = "\n队长当前不在线，上线后会看到这条消息。"
)
