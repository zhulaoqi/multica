package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	dispatchResultLookupTimeout = 10 * time.Second
	dispatchResultSendTimeout   = 15 * time.Second
)

// registerDispatchCompletionListener reports a department dispatch back to the
// conversation that asked for it. In-app requesters are handled inside
// OrgDispatchService (an inbox item); chat requesters get a message here. The
// service's notified_at claim makes this exactly-once even when status events
// repeat.
func registerDispatchCompletionListener(
	bus *events.Bus,
	queries *db.Queries,
	dispatcher *service.OrgDispatchService,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	if dispatcher == nil {
		return
	}
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if changed, _ := payload["status_changed"].(bool); !changed {
			return
		}
		issueID, workspaceID, status, ok := issueEventIdentity(payload)
		if !ok {
			return
		}
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			return
		}
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), dispatchResultLookupTimeout)
		defer cancel()
		if !service.IsDispatchCompleted(ctx, queries, wsUUID, status) {
			return
		}
		issue, err := queries.GetIssue(ctx, issueUUID)
		if err != nil {
			slog.Warn("dispatch completion: load issue failed", "issue_id", issueID, "error", err)
			return
		}
		notice, err := dispatcher.CompleteDispatch(ctx, issue)
		if err != nil {
			slog.Error("dispatch completion: notify failed", "issue_id", issueID, "error", err)
			return
		}
		if notice == nil || notice.Origin.ChannelType != service.DispatchChannelFeishu {
			return
		}
		go sendDispatchCompletionNotice(notice, queries, client, installs, appURL)
	})
}

// registerDispatchFailureListener reports a dispatch whose squad-leader run
// failed for good. Retries still pending are skipped, and the claim in
// FailDispatch only matches the leader task, so child task failures stay
// ordinary work rather than "the department gave up".
func registerDispatchFailureListener(
	bus *events.Bus,
	queries *db.Queries,
	dispatcher *service.OrgDispatchService,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	if dispatcher == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
		taskID, _ := payload["task_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		reason, _ := payload["failure_reason"].(string)
		if taskID == "" || issueID == "" {
			return
		}
		taskUUID, err := util.ParseUUID(taskID)
		if err != nil {
			return
		}
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), dispatchResultLookupTimeout)
		defer cancel()
		issue, err := queries.GetIssue(ctx, issueUUID)
		if err != nil {
			slog.Warn("dispatch failure: load issue failed", "issue_id", issueID, "error", err)
			return
		}
		notice, err := dispatcher.FailDispatch(ctx, issue, taskUUID, reason)
		if err != nil {
			slog.Error("dispatch failure: notify failed", "issue_id", issueID, "error", err)
			return
		}
		if notice == nil || notice.Origin.ChannelType != service.DispatchChannelFeishu {
			return
		}
		go sendDispatchFailureNotice(notice, queries, client, installs, appURL)
	})
}

// registerDispatchStartListener tells the requester once that the department
// actually picked the work up. The leader task running is the first moment the
// promise from the dispatch confirmation becomes true; StartDispatch's claim
// keeps retries and replayed events silent.
func registerDispatchStartListener(
	bus *events.Bus,
	queries *db.Queries,
	dispatcher *service.OrgDispatchService,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	if dispatcher == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskRunning, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		taskID, _ := payload["task_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		if taskID == "" || issueID == "" {
			return
		}
		taskUUID, err := util.ParseUUID(taskID)
		if err != nil {
			return
		}
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), dispatchResultLookupTimeout)
		defer cancel()
		task, err := queries.GetAgentTask(ctx, taskUUID)
		if err != nil || !task.IsLeaderTask {
			return
		}
		issue, err := queries.GetIssue(ctx, issueUUID)
		if err != nil {
			slog.Warn("dispatch start: load issue failed", "issue_id", issueID, "error", err)
			return
		}
		notice, err := dispatcher.StartDispatch(ctx, issue)
		if err != nil {
			slog.Error("dispatch start: notify failed", "issue_id", issueID, "error", err)
			return
		}
		if notice == nil || notice.Origin.ChannelType != service.DispatchChannelFeishu {
			return
		}
		go sendDispatchStartNotice(notice, queries, client, installs, appURL)
	})
}

// issueEventIdentity reads the fields every issue:updated projection carries,
// whether the publisher sent an IssueResponse or a resolved map.
func issueEventIdentity(payload map[string]any) (issueID, workspaceID, status string, ok bool) {
	switch issue := payload["issue"].(type) {
	case handler.IssueResponse:
		return issue.ID, issue.WorkspaceID, issue.Status, true
	case map[string]any:
		issueID, _ = issue["id"].(string)
		workspaceID, _ = issue["workspace_id"].(string)
		status, _ = issue["status"].(string)
		return issueID, workspaceID, status, issueID != "" && workspaceID != ""
	}
	return "", "", "", false
}

func sendDispatchCompletionNotice(
	notice *service.OrgDispatchNotice,
	queries *db.Queries,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	workspaceSlug := dispatchWorkspaceSlug(notice.Origin, queries)
	sendDispatchNotice(notice.Origin, notice.Issue.ID, "dispatch completion", client, queries, installs, func() string {
		return dispatchCompletionText(notice, workspaceSlug, appURL)
	})
}

func sendDispatchFailureNotice(
	notice *service.OrgDispatchFailureNotice,
	queries *db.Queries,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	workspaceSlug := dispatchWorkspaceSlug(notice.Origin, queries)
	sendDispatchNotice(notice.Origin, notice.Issue.ID, "dispatch failure", client, queries, installs, func() string {
		return dispatchFailureText(notice, workspaceSlug, appURL)
	})
}

func sendDispatchStartNotice(
	notice *service.ChatDispatchStartNotice,
	queries *db.Queries,
	client lark.APIClient,
	installs *lark.InstallationService,
	appURL string,
) {
	workspaceSlug := dispatchWorkspaceSlug(notice.Origin, queries)
	sendDispatchNotice(notice.Origin, notice.Issue.ID, "dispatch start", client, queries, installs, func() string {
		return dispatchStartText(notice, workspaceSlug, appURL)
	})
}

// sendDispatchNotice posts one dispatch message into its origin conversation.
// The origin message id (when present) keeps the message in the same Lark
// topic; SendTextWithReplyFallback only retries at chat level when Lark says
// the topic reply is impossible.
func sendDispatchNotice(
	origin db.DispatchOrigin,
	issueID pgtype.UUID,
	op string,
	client lark.APIClient,
	queries *db.Queries,
	installs *lark.InstallationService,
	text func() string,
) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dispatchResultSendTimeout)
	defer cancel()

	if !origin.ChatID.Valid || origin.ChatID.String == "" {
		slog.Warn(op+": origin has no chat id", "issue_id", util.UUIDToString(issueID))
		return
	}
	creds, err := dispatchReplyCredentials(ctx, queries, origin.InstallationID, origin.WorkspaceID, installs)
	if err != nil {
		slog.Error(op+": resolve reply bot failed", "issue_id", util.UUIDToString(issueID), "error", err)
		return
	}
	err = lark.SendTextWithReplyFallback(ctx, slog.Default(), op, client, lark.SendTextParams{
		InstallationID: creds,
		ChatID:         lark.ChatID(origin.ChatID.String),
		Text:           text(),
		ReplyTarget:    dispatchOriginReplyTarget(origin),
	})
	if err != nil {
		slog.Error(op+": send message failed",
			"issue_id", util.UUIDToString(issueID),
			"chat_id", origin.ChatID.String, "error", err)
	}
}

// dispatchOriginReplyTarget replies inside the topic of the message that
// created the dispatch. Origins recorded before the message id was persisted
// keep the old chat-level behavior.
func dispatchOriginReplyTarget(origin db.DispatchOrigin) lark.ReplyTarget {
	if !origin.OriginMessageID.Valid || origin.OriginMessageID.String == "" {
		return lark.ReplyTarget{}
	}
	return lark.ReplyTarget{
		MessageID: origin.OriginMessageID.String,
		InThread:  origin.ThreadID.Valid && origin.ThreadID.String != "",
	}
}

func dispatchWorkspaceSlug(origin db.DispatchOrigin, queries *db.Queries) string {
	ctx, cancel := context.WithTimeout(context.Background(), dispatchResultLookupTimeout)
	defer cancel()
	ws, err := queries.GetWorkspace(ctx, origin.WorkspaceID)
	if err != nil {
		return ""
	}
	return ws.Slug
}

func dispatchStartText(notice *service.ChatDispatchStartNotice, workspaceSlug, appURL string) string {
	line := fmt.Sprintf("部门开始处理：%s — %s", notice.Identifier, notice.Issue.Title)
	if notice.UnitName != "" {
		line += fmt.Sprintf("（部门：%s）", notice.UnitName)
	}
	line += "\n完成后我会在这里汇报。"
	if link := channel.IssueWebLink(appURL, workspaceSlug, notice.Identifier); link != "" {
		line += "\n" + link
	}
	return line
}

// dispatchReplyCredentials picks the bot that can post into the origin chat.
// Organization-bot origins store the org installation id, whose credentials
// come from the login app env; per-agent origins resolve a workspace-bound
// installation and decrypt its stored secret.
func dispatchReplyCredentials(
	ctx context.Context,
	queries *db.Queries,
	installationID pgtype.UUID,
	workspaceID pgtype.UUID,
	installs *lark.InstallationService,
) (lark.InstallationCredentials, error) {
	if installationID.Valid {
		org, err := queries.GetOrgChannelInstallationByID(ctx, installationID)
		switch {
		case err == nil && org.ChannelType == lark.OrgBotChannelType:
			appID := strings.TrimSpace(os.Getenv(orgBotAppIDEnv))
			appSecret := strings.TrimSpace(os.Getenv(orgBotAppSecretEnv))
			if appID == "" || appSecret == "" {
				return lark.InstallationCredentials{}, errors.New("organization bot credentials are not configured")
			}
			return lark.InstallationCredentials{
				AppID:     appID,
				AppSecret: appSecret,
				Region:    lark.RegionFeishu,
			}, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return lark.InstallationCredentials{}, fmt.Errorf("load org installation: %w", err)
		}
	}
	if installs == nil {
		return lark.InstallationCredentials{}, errors.New("installation service unavailable")
	}
	inst, err := installs.GetInWorkspace(ctx, installationID, workspaceID)
	if err != nil {
		return lark.InstallationCredentials{}, fmt.Errorf("load installation: %w", err)
	}
	secret, err := installs.DecryptAppSecret(inst)
	if err != nil {
		return lark.InstallationCredentials{}, fmt.Errorf("decrypt app secret: %w", err)
	}
	return lark.InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		TenantKey: inst.TenantKey.String,
		Region:    lark.Region(inst.Region),
	}, nil
}

func dispatchFailureText(notice *service.OrgDispatchFailureNotice, workspaceSlug, appURL string) string {
	line := fmt.Sprintf("派活失败：%s — %s", notice.Identifier, notice.Issue.Title)
	if notice.UnitName != "" {
		line += fmt.Sprintf("（部门：%s）", notice.UnitName)
	}
	if notice.Reason != "" {
		line += "\n原因：" + notice.Reason
	}
	if link := channel.IssueWebLink(appURL, workspaceSlug, notice.Identifier); link != "" {
		line += "\n" + link
	}
	return line
}

func dispatchCompletionText(notice *service.OrgDispatchNotice, workspaceSlug, appURL string) string {
	line := fmt.Sprintf("派活完成：%s — %s（状态：%s）",
		notice.Identifier, notice.Issue.Title, statusLabel(notice.Issue.Status))
	if link := channel.IssueWebLink(appURL, workspaceSlug, notice.Identifier); link != "" {
		line += "\n" + link
	}
	return line
}
