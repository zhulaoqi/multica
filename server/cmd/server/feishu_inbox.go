package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// inbox types that already send their own Feishu card before the inbox row
// is written. Mirroring those would deliver the same notice twice.
var feishuInboxAlreadySent = map[string]bool{
	"intake_blocker": true,
}

var feishuInboxLabel = map[string]string{
	"issue_assigned":     "任务分配给你",
	"unassigned":         "任务已从你名下移走",
	"assignee_changed":   "负责人变了",
	"status_changed":     "状态变了",
	"new_comment":        "有新评论",
	"mentioned":          "有人提到你",
	"priority_changed":   "优先级变了",
	"start_date_changed": "开始日期变了",
	"due_date_changed":   "截止日期变了",
	"task_completed":     "智能体运行完成",
	"task_failed":        "智能体运行失败",
	"agent_blocked":      "智能体被挡住",
	"agent_completed":    "智能体完成了",
	"reaction_added":     "有人回应了你",
}

type feishuInboxTarget struct {
	creds  lark.InstallationCredentials
	openID string
}

// registerFeishuInboxMirror sends a personal Feishu message for every member
// inbox item. The in-app inbox stays. A person with no Feishu identity is skipped.
func registerFeishuInboxMirror(bus *events.Bus, queries *db.Queries, h *handler.Handler) {
	client := h.LarkAPIClient
	appID := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_ID"))
	appSecret := strings.TrimSpace(os.Getenv("MULTICA_FEISHU_APP_SECRET"))
	if client == nil && appID != "" && appSecret != "" {
		client = lark.NewHTTPAPIClient(lark.HTTPClientConfig{Logger: slog.Default()})
	}
	if client == nil && h.LarkInstallations == nil {
		return
	}
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		item, ok := payload["item"].(map[string]any)
		if !ok {
			return
		}
		go deliverFeishuInbox(context.Background(), queries, h.LarkInstallations, client, appID, appSecret, item)
	})
}

func deliverFeishuInbox(ctx context.Context, queries *db.Queries, installations *lark.InstallationService, client lark.APIClient, appID, appSecret string, item map[string]any) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	target, ok := resolveFeishuInboxTarget(ctx, queries, installations, appID, appSecret, item)
	if !ok {
		return
	}
	card, err := feishuInboxCard(ctx, queries, item)
	if err != nil || card == "" {
		return
	}
	if err := lark.SendPersonalCard(client, ctx, target.creds, target.openID, card); err != nil {
		slog.Warn("feishu inbox mirror failed", "error", err, "type", mapString(item, "type"))
	}
}

func resolveFeishuInboxTarget(ctx context.Context, queries *db.Queries, installations *lark.InstallationService, appID, appSecret string, item map[string]any) (feishuInboxTarget, bool) {
	if mapString(item, "recipient_type") != "member" {
		return feishuInboxTarget{}, false
	}
	if feishuInboxAlreadySent[mapString(item, "type")] {
		return feishuInboxTarget{}, false
	}
	recipientID, err := util.ParseUUID(mapString(item, "recipient_id"))
	workspaceID, wsErr := util.ParseUUID(mapString(item, "workspace_id"))
	if err != nil || wsErr != nil || !recipientID.Valid || !workspaceID.Valid {
		return feishuInboxTarget{}, false
	}
	if installations != nil {
		binding, err := queries.FindLarkOpenIDForUser(ctx, db.FindLarkOpenIDForUserParams{
			WorkspaceID: workspaceID, MulticaUserID: recipientID,
		})
		if err == nil && binding.LarkOpenID != "" {
			inst, err := installations.GetInWorkspace(ctx, binding.InstallationID, workspaceID)
			if err == nil {
				secret, err := installations.DecryptAppSecret(inst)
				if err == nil && secret != "" {
					return feishuInboxTarget{
						openID: binding.LarkOpenID,
						creds: lark.InstallationCredentials{
							AppID: inst.AppID, AppSecret: secret, TenantKey: inst.TenantKey.String, Region: lark.Region(inst.Region),
						},
					}, true
				}
			}
		} else if err != nil && !isNoRows(err) {
			slog.Warn("feishu inbox mirror: lark binding lookup failed", "error", err)
		}
	}
	if appID == "" || appSecret == "" {
		return feishuInboxTarget{}, false
	}
	ident, err := queries.GetFeishuIdentityByUserID(ctx, recipientID)
	if err != nil {
		if !isNoRows(err) {
			slog.Warn("feishu inbox mirror: identity lookup failed", "error", err)
		}
		return feishuInboxTarget{}, false
	}
	if ident.OpenID == "" {
		return feishuInboxTarget{}, false
	}
	return feishuInboxTarget{
		openID: ident.OpenID,
		creds:  lark.InstallationCredentials{AppID: appID, AppSecret: appSecret, Region: lark.RegionFeishu},
	}, true
}

func feishuInboxCard(ctx context.Context, queries *db.Queries, item map[string]any) (string, error) {
	kind := mapString(item, "type")
	title := feishuInboxLabel[kind]
	if title == "" {
		title = "有一条新通知"
	}
	subject := mapString(item, "title")
	body := inboxMapBody(item)
	var text string
	switch {
	case subject != "" && body != "":
		text = subject + "\n" + body
	case subject != "":
		text = subject
	default:
		text = body
	}
	if text == "" {
		text = title
	}
	link := feishuInboxLink(ctx, queries, item)
	template := "blue"
	if mapString(item, "severity") == "action_required" || mapString(item, "severity") == "action" {
		template = "orange"
	}
	button := "打开"
	if link == "" {
		button = "知道了"
		link = appURLFromEnv()
	}
	if link == "" {
		link = "https://open.feishu.cn"
	}
	return lark.IntakeCard(template, title, text, button, link)
}

func feishuInboxLink(ctx context.Context, queries *db.Queries, item map[string]any) string {
	base := strings.TrimRight(appURLFromEnv(), "/")
	if base == "" {
		return ""
	}
	workspaceID := mapString(item, "workspace_id")
	slug := workspaceID
	if id, err := util.ParseUUID(workspaceID); err == nil && id.Valid {
		if ws, err := queries.GetWorkspace(ctx, id); err == nil && ws.Slug != "" {
			slug = ws.Slug
		}
	}
	if slug == "" {
		return base + "/inbox"
	}
	link := base + "/" + slug + "/inbox"
	if issueID := mapString(item, "issue_id"); issueID != "" {
		link += "?issue=" + issueID
	}
	return link
}

func inboxMapBody(item map[string]any) string {
	switch v := item["body"].(type) {
	case *string:
		if v != nil {
			return *v
		}
	case string:
		return v
	}
	return ""
}

func mapString(item map[string]any, key string) string {
	switch v := item[key].(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}
