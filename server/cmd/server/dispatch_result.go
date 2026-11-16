package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	if client == nil || installs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dispatchResultSendTimeout)
	defer cancel()

	if !notice.Origin.ChatID.Valid || notice.Origin.ChatID.String == "" {
		slog.Warn("dispatch completion: origin has no chat id", "issue_id", util.UUIDToString(notice.Issue.ID))
		return
	}
	inst, err := installs.GetInWorkspace(ctx, notice.Origin.InstallationID, notice.Origin.WorkspaceID)
	if err != nil {
		slog.Error("dispatch completion: load installation failed",
			"issue_id", util.UUIDToString(notice.Issue.ID), "error", err)
		return
	}
	secret, err := installs.DecryptAppSecret(inst)
	if err != nil {
		slog.Error("dispatch completion: decrypt app secret failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		return
	}
	slug := ""
	if ws, err := queries.GetWorkspace(ctx, notice.Origin.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	_, err = client.SendTextMessage(ctx, lark.SendTextParams{
		InstallationID: lark.InstallationCredentials{
			AppID:     inst.AppID,
			AppSecret: secret,
			TenantKey: inst.TenantKey.String,
			Region:    lark.Region(inst.Region),
		},
		ChatID: lark.ChatID(notice.Origin.ChatID.String),
		Text:   dispatchCompletionText(notice, slug, appURL),
	})
	if err != nil {
		slog.Error("dispatch completion: send message failed",
			"issue_id", util.UUIDToString(notice.Issue.ID),
			"chat_id", notice.Origin.ChatID.String, "error", err)
	}
}

func dispatchCompletionText(notice *service.OrgDispatchNotice, workspaceSlug, appURL string) string {
	line := fmt.Sprintf("派活完成：%s — %s（状态：%s）",
		notice.Identifier, notice.Issue.Title, statusLabel(notice.Issue.Status))
	if link := channel.IssueWebLink(appURL, workspaceSlug, notice.Identifier); link != "" {
		line += "\n" + link
	}
	return line
}
