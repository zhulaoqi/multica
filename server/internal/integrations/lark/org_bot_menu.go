package lark

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Feishu's floating menu allows ten children under one top-level entry.
// One extra visible department or project is replaced by 更多, which opens
// the full card list.
const orgBotMenuChildCap = 10

// orgBotMenuMemory remembers, for this process, which private chat belongs
// to a person, which menu was last published, and a selection made before
// that chat id was known. Feishu's menu click does not include a chat id.
type orgBotMenuMemory struct {
	mu           sync.Mutex
	chatByOpenID map[string]ChatID
	pinByOpenID  map[string]OrgBotChatContext
	fingerprint  map[string]string
}

func (b *OrgBot) noteP2PChat(msg InboundMessage) {
	if msg.ChatType != ChatTypeP2P || msg.ChatID == "" || msg.SenderOpenID == "" {
		return
	}
	if msg.MessageType == orgBotMenuActionType {
		return
	}
	openID := string(msg.SenderOpenID)
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	if b.menu.chatByOpenID == nil {
		b.menu.chatByOpenID = map[string]ChatID{}
	}
	b.menu.chatByOpenID[openID] = msg.ChatID
}

func (b *OrgBot) knownP2PChat(openID OpenID) (ChatID, bool) {
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	chatID, ok := b.menu.chatByOpenID[string(openID)]
	return chatID, ok && chatID != ""
}

func (b *OrgBot) storeMenuPin(openID OpenID, state OrgBotChatContext) {
	if openID == "" || !state.hasConcreteTarget() {
		return
	}
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	if b.menu.pinByOpenID == nil {
		b.menu.pinByOpenID = map[string]OrgBotChatContext{}
	}
	b.menu.pinByOpenID[string(openID)] = state
}

func (b *OrgBot) clearMenuPin(openID OpenID) {
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	delete(b.menu.pinByOpenID, string(openID))
}

func (b *OrgBot) takeMenuPin(openID OpenID) (OrgBotChatContext, bool) {
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	pin, ok := b.menu.pinByOpenID[string(openID)]
	if ok {
		delete(b.menu.pinByOpenID, string(openID))
	}
	return pin, ok && pin.hasConcreteTarget()
}

func applyMenuPin(state, pin OrgBotChatContext) OrgBotChatContext {
	state.Kind = pin.Kind
	state.UnitID = pin.UnitID
	state.UnitPath = pin.UnitPath
	state.ProjectID = pin.ProjectID
	state.ProjectName = pin.ProjectName
	state.Awaiting = ""
	state.PendingText = ""
	state.Candidates = nil
	return state
}

func (b *OrgBot) applyMenuKey(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, key string) (string, DispatchResult, OrgBotChatContext, error) {
	kind, rawID, more, ok := parseOrgBotMenuKey(key)
	if !ok {
		return "这个菜单已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	if more {
		if kind == orgBotTargetProject {
			return b.listProjects(ctx, msg, userID, org, state)
		}
		return b.offerTargets(ctx, msg, userID, org, state, strings.TrimSpace(state.PendingText) != "")
	}
	var (
		reply  string
		result DispatchResult
		next   OrgBotChatContext
		err    error
	)
	switch kind {
	case orgBotTargetPersonal:
		reply, result, next, err = b.choosePersonal(ctx, msg, userID, org, state)
	case orgBotTargetProject:
		reply, result, next, err = b.chooseProjectID(ctx, msg, userID, org, state, rawID)
	case orgBotTargetDepartment:
		reply, result, next, err = b.chooseMenuDepartment(ctx, msg, userID, org, state, rawID)
	default:
		return "这个菜单已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	if err == nil && msg.ChatID == "" && next.hasConcreteTarget() {
		b.storeMenuPin(msg.SenderOpenID, next)
	}
	return reply, result, next, err
}

func (b *OrgBot) chooseMenuDepartment(ctx context.Context, msg InboundMessage, userID pgtype.UUID, org db.Workspace, state OrgBotChatContext, raw string) (string, DispatchResult, OrgBotChatContext, error) {
	id, err := util.ParseUUID(raw)
	if err != nil || b.cfg.Departments == nil {
		return "这个部门已失效。回复「换目标」重新选择。", DispatchResult{Outcome: OutcomeDispatchUsage}, state, nil
	}
	units, err := b.cfg.Departments.ListRequestableDepartments(ctx, org.ID, userID)
	if err != nil {
		return "", DispatchResult{}, state, err
	}
	want := util.UUIDToString(id)
	for _, unit := range units {
		if strings.TrimSpace(unit.UnitID) != want {
			continue
		}
		return b.finishPin(ctx, msg, userID, org, state, service.UnitMatch{
			Unit: db.ListOrgUnitsRow{ID: id},
			Path: unit.Path,
		})
	}
	return "你不能把工作交给这个部门。回复「换目标」看可以选的。", DispatchResult{Outcome: OutcomeDispatchNoAccess}, state, nil
}

func (b *OrgBot) refreshMenu(ctx context.Context, msg InboundMessage) (string, DispatchResult, error) {
	userID, unbound, err := b.senderUser(ctx, msg)
	if err != nil {
		return "", DispatchResult{}, err
	}
	if unbound {
		return "", DispatchResult{Outcome: OutcomeNeedsBinding}, nil
	}
	org, err := b.cfg.Organization.GetOrganizationWorkspace(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", DispatchResult{Outcome: OutcomeDispatchNoAccess}, nil
	}
	if err != nil {
		return "", DispatchResult{}, fmt.Errorf("load organization workspace: %w", err)
	}
	b.publishMenu(ctx, msg, userID, org.ID)
	// Feishu reports the newest message this person can still see. A value
	// means the history is still there, so reopening the chat stays quiet.
	// An empty value is a first open or a cleared history.
	if strings.TrimSpace(msg.LastVisibleMessageID) != "" {
		return "", DispatchResult{Outcome: OutcomeDispatchUsage}, nil
	}
	post, err := orgBotOpenPost()
	if err != nil {
		return "", DispatchResult{}, err
	}
	return post, DispatchResult{Outcome: OutcomeDispatchUsage}, nil
}

func (b *OrgBot) publishMenu(ctx context.Context, msg InboundMessage, userID, orgID pgtype.UUID) {
	publisher, ok := b.cfg.Client.(BotMenuPublisher)
	if !ok || msg.SenderOpenID == "" {
		return
	}
	var units []service.RouteCandidate
	if b.cfg.Departments != nil {
		listed, err := b.cfg.Departments.ListRequestableDepartments(ctx, orgID, userID)
		if err != nil {
			b.cfg.Logger.Warn("lark org bot: menu departments", "error", err)
			return
		}
		units = listed
	}
	var projects []service.RequestableProject
	if b.cfg.Projects != nil {
		listed, err := b.cfg.Projects.ListRequestableProjects(ctx, orgID, userID)
		if err != nil {
			b.cfg.Logger.Warn("lark org bot: menu projects", "error", err)
			return
		}
		projects = listed
	}
	items := orgBotMenuItems(units, projects)
	fingerprint := orgBotMenuFingerprint(items)
	openID := string(msg.SenderOpenID)
	if b.sameMenu(openID, fingerprint) {
		return
	}
	if err := publisher.PublishUserBotMenu(ctx, b.cfg.Credentials, openID, items); err != nil {
		b.cfg.Logger.Warn("lark org bot: publish menu", "error", err)
		return
	}
	b.rememberMenu(openID, fingerprint)
}

func (b *OrgBot) sameMenu(openID, fingerprint string) bool {
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	return b.menu.fingerprint[openID] == fingerprint && fingerprint != ""
}

func (b *OrgBot) rememberMenu(openID, fingerprint string) {
	b.menu.mu.Lock()
	defer b.menu.mu.Unlock()
	if b.menu.fingerprint == nil {
		b.menu.fingerprint = map[string]string{}
	}
	b.menu.fingerprint[openID] = fingerprint
}

func (b *OrgBot) sendPersonalText(ctx context.Context, msg InboundMessage, text string) error {
	card, err := orgBotNoticeCard("blue", "易研", []orgBotCardField{{Label: "说明", Value: text}}, nil, "", "")
	if err != nil || card == "" {
		return err
	}
	return SendPersonalCard(b.cfg.Client, ctx, b.cfg.Credentials, string(msg.SenderOpenID), card)
}

func orgBotMenuItems(units []service.RouteCandidate, projects []service.RequestableProject) []BotMenuItem {
	items := []BotMenuItem{{Name: "我的小队", EventKey: "s"}}
	if children := departmentMenuChildren(units); len(children) > 0 {
		items = append(items, BotMenuItem{Name: "部门", Children: children})
	}
	if children := projectMenuChildren(projects); len(children) > 0 {
		items = append(items, BotMenuItem{Name: "项目", Children: children})
	}
	return items
}

func departmentMenuChildren(units []service.RouteCandidate) []BotMenuItem {
	items := make([]BotMenuItem, 0, len(units))
	for _, unit := range units {
		label := menuLabel(unit.Path)
		id, err := util.ParseUUID(strings.TrimSpace(unit.UnitID))
		if label == "" || err != nil {
			continue
		}
		key, ok := menuIDKey("d", id)
		if !ok {
			continue
		}
		items = append(items, BotMenuItem{Name: label, EventKey: key})
	}
	return capMenuChildren(items, "md")
}

func projectMenuChildren(projects []service.RequestableProject) []BotMenuItem {
	items := make([]BotMenuItem, 0, len(projects))
	for _, project := range projects {
		label := menuLabel(project.Name)
		key, ok := menuIDKey("p", project.ID)
		if label == "" || !ok {
			continue
		}
		items = append(items, BotMenuItem{Name: label, EventKey: key})
	}
	return capMenuChildren(items, "mp")
}

func capMenuChildren(items []BotMenuItem, moreKey string) []BotMenuItem {
	if len(items) <= orgBotMenuChildCap {
		return items
	}
	kept := append([]BotMenuItem{}, items[:orgBotMenuChildCap-1]...)
	kept = append(kept, BotMenuItem{Name: "更多", EventKey: moreKey})
	return kept
}

func menuLabel(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) > 60 {
		runes = runes[:60]
	}
	if len(runes) < 2 {
		return ""
	}
	return string(runes)
}

func menuIDKey(prefix string, id pgtype.UUID) (string, bool) {
	if prefix == "" || !id.Valid {
		return "", false
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id.Bytes[:])
	key := prefix + encoded
	if len(key) < 1 || len(key) > 30 {
		return "", false
	}
	return key, true
}

func parseOrgBotMenuKey(key string) (kind, rawID string, more bool, ok bool) {
	key = strings.TrimSpace(key)
	switch key {
	case "s":
		return orgBotTargetPersonal, "", false, true
	case "md":
		return orgBotTargetDepartment, "", true, true
	case "mp":
		return orgBotTargetProject, "", true, true
	}
	if len(key) < 2 {
		return "", "", false, false
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(key[1:]))
	if err != nil || len(decoded) != 16 {
		return "", "", false, false
	}
	var id pgtype.UUID
	copy(id.Bytes[:], decoded)
	id.Valid = true
	switch key[:1] {
	case "d":
		return orgBotTargetDepartment, util.UUIDToString(id), false, true
	case "p":
		return orgBotTargetProject, util.UUIDToString(id), false, true
	default:
		return "", "", false, false
	}
}

func orgBotMenuFingerprint(items []BotMenuItem) string {
	raw, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(raw)
}

func userBotMenuBody(openID string, items []BotMenuItem) (map[string]any, error) {
	encoded := make([]any, 0, len(items))
	for _, item := range items {
		node, err := encodeBotMenuItem(item)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, node)
	}
	return map[string]any{
		"user_id": openID,
		"bot_menu": map[string]any{
			"bot_menu_items": encoded,
		},
	}, nil
}

func encodeBotMenuItem(item BotMenuItem) (map[string]any, error) {
	name := menuLabel(item.Name)
	if name == "" {
		return nil, errBotMenuItem
	}
	node := map[string]any{
		"name":      name,
		"i18n_name": map[string]string{"zh_cn": name, "en_us": name},
	}
	if len(item.Children) > 0 {
		children := make([]any, 0, len(item.Children))
		for _, child := range item.Children {
			encoded, err := encodeBotMenuItem(child)
			if err != nil {
				return nil, err
			}
			children = append(children, encoded)
		}
		node["children"] = children
		return node, nil
	}
	key := strings.TrimSpace(item.EventKey)
	if key == "" || len(key) > 30 {
		return nil, errBotMenuItem
	}
	node["behaviors"] = []any{
		map[string]any{"type": "event_key", "event_key": key, "is_primary": true},
	}
	return node, nil
}

var errBotMenuItem = errors.New("lark http client: invalid bot menu item")
