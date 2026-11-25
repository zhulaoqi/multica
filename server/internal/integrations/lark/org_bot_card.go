package lark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/service"
)

const orgBotChoiceButtonLimit = 8

type orgBotCardButton struct {
	Label  string
	Action string
}

// NewOrgBotFrameDecoder decodes chat messages and, for the organization bot
// only, card button clicks and the input-bar menu. Per-agent bots keep the
// message decoder, which still ignores those events.
func NewOrgBotFrameDecoder() FrameDecoder { return orgBotFrameDecoder{} }

type orgBotFrameDecoder struct{}

func (orgBotFrameDecoder) Decode(payload []byte, inst Installation) (InboundMessage, bool, error) {
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(payload, inst)
	if err != nil || ok {
		return msg, ok, err
	}
	msg, ok, err = decodeOrgBotCardAction(payload)
	if err != nil || ok {
		return msg, ok, err
	}
	msg, ok, err = decodeOrgBotMenuAction(payload)
	if err != nil || ok {
		return msg, ok, err
	}
	return decodeOrgBotChatEntered(payload)
}

func decodeOrgBotCardAction(payload []byte) (InboundMessage, bool, error) {
	var env larkEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return InboundMessage{}, false, nil
	}
	if env.Header.EventType != "card.action.trigger" {
		return InboundMessage{}, false, nil
	}
	if len(env.Event) == 0 {
		return InboundMessage{}, false, errors.New("card action missing event")
	}
	var evt struct {
		Operator struct {
			OpenID string `json:"open_id"`
		} `json:"operator"`
		Action struct {
			Value json.RawMessage `json:"value"`
		} `json:"action"`
		Context struct {
			OpenChatID string `json:"open_chat_id"`
		} `json:"context"`
	}
	if err := json.Unmarshal(env.Event, &evt); err != nil {
		return InboundMessage{}, false, fmt.Errorf("card action: %w", err)
	}
	action, ok := cardActionToken(evt.Action.Value)
	if !ok || evt.Context.OpenChatID == "" || evt.Operator.OpenID == "" || env.Header.EventID == "" {
		return InboundMessage{}, false, nil
	}
	return InboundMessage{
		EventType:      env.Header.EventType,
		EventID:        env.Header.EventID,
		AppID:          env.Header.AppID,
		ChatID:         ChatID(evt.Context.OpenChatID),
		ChatType:       ChatTypeP2P,
		MessageID:      env.Header.EventID,
		SenderOpenID:   OpenID(evt.Operator.OpenID),
		MessageType:    orgBotCardActionType,
		CommandBody:    action,
		AddressedToBot: true,
		CreateTime:     env.Header.CreateTime,
	}, true, nil
}

func decodeOrgBotMenuAction(payload []byte) (InboundMessage, bool, error) {
	var env larkEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return InboundMessage{}, false, nil
	}
	if env.Header.EventType != "application.bot.menu_v6" {
		return InboundMessage{}, false, nil
	}
	if len(env.Event) == 0 {
		return InboundMessage{}, false, errors.New("bot menu missing event")
	}
	var evt struct {
		Operator struct {
			OperatorID struct {
				OpenID string `json:"open_id"`
			} `json:"operator_id"`
		} `json:"operator"`
		EventKey string `json:"event_key"`
	}
	if err := json.Unmarshal(env.Event, &evt); err != nil {
		return InboundMessage{}, false, fmt.Errorf("bot menu: %w", err)
	}
	key := strings.TrimSpace(evt.EventKey)
	openID := strings.TrimSpace(evt.Operator.OperatorID.OpenID)
	if key == "" || openID == "" || env.Header.EventID == "" {
		return InboundMessage{}, false, nil
	}
	return InboundMessage{
		EventType:      env.Header.EventType,
		EventID:        env.Header.EventID,
		AppID:          env.Header.AppID,
		ChatType:       ChatTypeP2P,
		MessageID:      env.Header.EventID,
		SenderOpenID:   OpenID(openID),
		MessageType:    orgBotMenuActionType,
		CommandBody:    key,
		AddressedToBot: true,
		CreateTime:     env.Header.CreateTime,
	}, true, nil
}

func decodeOrgBotChatEntered(payload []byte) (InboundMessage, bool, error) {
	var env larkEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return InboundMessage{}, false, nil
	}
	if env.Header.EventType != "im.chat.access_event.bot_p2p_chat_entered_v1" {
		return InboundMessage{}, false, nil
	}
	if len(env.Event) == 0 {
		return InboundMessage{}, false, errors.New("chat entered missing event")
	}
	var evt struct {
		ChatID        string `json:"chat_id"`
		LastMessageID string `json:"last_message_id"`
		OperatorID    struct {
			OpenID string `json:"open_id"`
		} `json:"operator_id"`
	}
	if err := json.Unmarshal(env.Event, &evt); err != nil {
		return InboundMessage{}, false, fmt.Errorf("chat entered: %w", err)
	}
	openID := strings.TrimSpace(evt.OperatorID.OpenID)
	chatID := strings.TrimSpace(evt.ChatID)
	if openID == "" || chatID == "" || env.Header.EventID == "" {
		return InboundMessage{}, false, nil
	}
	return InboundMessage{
		EventType:            env.Header.EventType,
		EventID:              env.Header.EventID,
		AppID:                env.Header.AppID,
		ChatID:               ChatID(chatID),
		ChatType:             ChatTypeP2P,
		MessageID:            env.Header.EventID,
		SenderOpenID:         OpenID(openID),
		MessageType:          orgBotChatEnteredType,
		AddressedToBot:       true,
		CreateTime:           env.Header.CreateTime,
		LastVisibleMessageID: strings.TrimSpace(evt.LastMessageID),
	}, true, nil
}

func cardActionToken(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	action, _ := obj["action"].(string)
	action = strings.TrimSpace(action)
	return action, action != ""
}

type orgBotCardField struct {
	Label string
	Value string
}

// orgBotChoiceCard is the tap list. The header is the only title; the numbered
// text reply stays outside the card for clients that cannot click buttons.
// The options are the content, so each one spans the card.
func orgBotChoiceCard(title string, buttons []orgBotCardButton) (string, error) {
	elements := make([]any, 0, len(buttons))
	for _, button := range buttons {
		elements = append(elements, orgBotCallbackButton(button, "fill"))
	}
	return orgBotCardJSON("blue", title, elements)
}

func orgBotSelectedCard(state OrgBotChatContext, allowSwitch bool) (string, error) {
	label := strings.TrimSpace(state.label())
	title := "已选中"
	if label != "" {
		title = "已选中 " + label
	}
	fields := []orgBotCardField{{Label: "下一步", Value: "直接发送需求"}}
	if state.Kind == orgBotTargetProject {
		if path := strings.TrimSpace(state.UnitPath); path != "" {
			fields = append([]orgBotCardField{{Label: "交给", Value: path}}, fields...)
		}
	}
	return orgBotNoticeCard("blue", title, fields, orgBotSwitchButtons(allowSwitch), "", "")
}

func orgBotDispatchedCard(label, requirement, identifier, handedTo, link, attachment string, allowSwitch bool) (string, error) {
	title := "已交给"
	label = strings.TrimSpace(label)
	if label != "" {
		title = "已交给 " + label
	}
	fields := []orgBotCardField{
		{Label: "需求", Value: requirement},
		{Label: "编号", Value: identifier},
	}
	handedTo = strings.TrimSpace(handedTo)
	if handedTo != "" && handedTo != label && !strings.Contains(label, handedTo) {
		fields = append(fields, orgBotCardField{Label: "交给", Value: handedTo})
	}
	if note := strings.TrimSpace(attachment); note != "" {
		fields = append(fields, orgBotCardField{Label: "附件", Value: note})
	}
	return orgBotNoticeCard("green", title, fields, orgBotSwitchButtons(allowSwitch), "打开任务", link)
}

// OrgDispatchWorker is who is doing a dispatched task, as the receipt cards
// show it: where the request was handed and the agent running it. Empty parts
// are left off the card.
type OrgDispatchWorker struct {
	// Kind is one of the service.DispatchHandoff* kinds.
	Kind     string
	HandedTo string
	Agent    string
}

// NewOrgDispatchWorker words a handoff for one reader. forRequester is true
// when the card goes to the person who asked for the work.
func NewOrgDispatchWorker(handoff service.DispatchHandoff, forRequester bool, agent string) OrgDispatchWorker {
	return OrgDispatchWorker{
		Kind:     handoff.Kind,
		HandedTo: orgDispatchHandedTo(handoff, forRequester),
		Agent:    strings.TrimSpace(agent),
	}
}

func (w OrgDispatchWorker) fields() []orgBotCardField {
	return []orgBotCardField{
		{Label: "交给", Value: w.HandedTo},
		{Label: "智能体", Value: w.Agent},
	}
}

// orgDispatchHandedTo words a handoff the way the 已交给 card does. A personal
// squad reads 我的小队 only to the person who owns it.
func orgDispatchHandedTo(handoff service.DispatchHandoff, forRequester bool) string {
	name := strings.TrimSpace(handoff.Name)
	switch handoff.Kind {
	case service.DispatchHandoffProject:
		if name == "" {
			return "项目"
		}
		return "项目 " + name
	case service.DispatchHandoffSquad:
		if forRequester || name == "" {
			return "我的小队"
		}
		return "小队 " + name
	case service.DispatchHandoffDepartment:
		if name == "" {
			return "部门"
		}
		return name
	default:
		return ""
	}
}

func orgDispatchStartTitle(kind string) string {
	switch kind {
	case service.DispatchHandoffProject:
		return "项目开始处理"
	case service.DispatchHandoffSquad:
		return "小队开始处理"
	case service.DispatchHandoffDepartment:
		return "部门开始处理"
	default:
		return "开始处理"
	}
}

// OrgDispatchStartCard is the notice sent when the work is picked up.
func OrgDispatchStartCard(identifier, title string, worker OrgDispatchWorker, link string) (string, error) {
	fields := append([]orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
	}, worker.fields()...)
	return orgBotNoticeCard("blue", orgDispatchStartTitle(worker.Kind), fields, orgBotSwitchButtons(true), "打开任务", link)
}

// OrgDispatchDoneCard is the notice sent when that work is finished.
func OrgDispatchDoneCard(identifier, title, status string, worker OrgDispatchWorker, link string) (string, error) {
	fields := append([]orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
	}, worker.fields()...)
	fields = append(fields, orgBotCardField{Label: "状态", Value: orgBotStatusLabel(status)})
	return orgBotNoticeCard("green", "已完成", fields, orgBotSwitchButtons(true), "打开任务", link)
}

// OrgDispatchFailedCard is the notice sent when the leader run failed for good.
func OrgDispatchFailedCard(identifier, title string, worker OrgDispatchWorker, reason, link string) (string, error) {
	fields := append([]orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
	}, worker.fields()...)
	fields = append(fields, orgBotCardField{Label: "原因", Value: reason})
	return orgBotNoticeCard("red", "派活失败", fields, orgBotSwitchButtons(true), "打开任务", link)
}

func orgBotSwitchButtons(allow bool) []orgBotCardButton {
	if !allow {
		return nil
	}
	return []orgBotCardButton{{Label: "切换", Action: "target:menu"}}
}

func orgBotStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "backlog", "todo":
		return "待处理"
	case "in_progress":
		return "处理中"
	case "in_review":
		return "待验收"
	case "done":
		return "已完成"
	case "blocked":
		return "阻塞"
	case "cancelled":
		return "已取消"
	default:
		return strings.TrimSpace(status)
	}
}

// orgBotFieldLabelWidth fits a four-character label, so values start on one
// line down the card whatever the label length.
const orgBotFieldLabelWidth = "56px"

func orgBotNoticeCard(template, title string, fields []orgBotCardField, buttons []orgBotCardButton, openLabel, openURL string) (string, error) {
	elements := make([]any, 0, len(fields)+1)
	for _, field := range fields {
		value := cardPlain(field.Value)
		if value == "" {
			continue
		}
		elements = append(elements, orgBotFieldRow(field.Label, value))
	}
	if actions := orgBotActionRow(buttons, openLabel, openURL, len(elements) > 0); actions != nil {
		elements = append(elements, actions)
	}
	return orgBotCardJSON(template, title, elements)
}

func orgBotCardJSON(template, title string, elements []any) (string, error) {
	if len(elements) == 0 {
		return "", nil
	}
	doc := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": trimCardTitle(title)},
		},
		"body": map[string]any{
			"vertical_spacing": "8px",
			"elements":         elements,
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func orgBotFieldRow(label, value string) map[string]any {
	return map[string]any{
		"tag":                "column_set",
		"flex_mode":          "none",
		"horizontal_spacing": "8px",
		"columns": []any{
			map[string]any{
				"tag":            "column",
				"width":          orgBotFieldLabelWidth,
				"vertical_align": "top",
				"elements": []any{
					map[string]any{"tag": "markdown", "content": "<font color='grey'>" + cardPlain(label) + "</font>"},
				},
			},
			map[string]any{
				"tag":            "column",
				"width":          "weighted",
				"weight":         1,
				"vertical_align": "top",
				"elements": []any{
					map[string]any{"tag": "markdown", "content": value},
				},
			},
		},
	}
}

// orgBotActionRow is the footer: the open-task button is the one filled
// action and the callbacks follow it as outlined buttons. Each button keeps
// its content width, and the row wraps on a narrow screen.
func orgBotActionRow(buttons []orgBotCardButton, openLabel, openURL string, afterFields bool) any {
	columns := make([]any, 0, len(buttons)+1)
	if url := strings.TrimSpace(openURL); url != "" {
		label := strings.TrimSpace(openLabel)
		if label == "" {
			label = "打开"
		}
		columns = append(columns, orgBotButtonColumn(orgBotButton(label, "primary_filled", "default", nil, map[string]any{
			"type": "open_url", "default_url": url,
		})))
	}
	for _, button := range buttons {
		columns = append(columns, orgBotButtonColumn(orgBotCallbackButton(button, "default")))
	}
	if len(columns) == 0 {
		return nil
	}
	row := map[string]any{
		"tag":                "column_set",
		"flex_mode":          "flow",
		"horizontal_spacing": "8px",
		"horizontal_align":   "left",
		"columns":            columns,
	}
	if afterFields {
		// Body spacing is 8px; the extra 8px sets the actions apart from the fields.
		row["margin"] = "8px 0px 0px 0px"
	}
	return row
}

func orgBotCallbackButton(button orgBotCardButton, width string) map[string]any {
	action := map[string]string{"action": button.Action}
	return orgBotButton(button.Label, "default", width, action, map[string]any{
		"type": "callback", "value": action,
	})
}

func orgBotButton(label, kind, width string, value map[string]string, behavior map[string]any) map[string]any {
	button := map[string]any{
		"tag":       "button",
		"text":      map[string]any{"tag": "plain_text", "content": trimCardButton(label)},
		"type":      kind,
		"width":     width,
		"size":      "medium",
		"behaviors": []any{behavior},
	}
	if value != nil {
		button["value"] = value
	}
	return button
}

func orgBotButtonColumn(button map[string]any) map[string]any {
	return map[string]any{
		"tag":            "column",
		"width":          "auto",
		"vertical_align": "center",
		"elements":       []any{button},
	}
}

func trimCardTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "易研"
	}
	runes := []rune(title)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return title
}

func cardPlain(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\\', '*', '_', '`', '[', ']', '<', '>', '~':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func trimCardButton(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "选择"
	}
	runes := []rune(label)
	if len(runes) > 30 {
		return string(runes[:30])
	}
	return label
}

func orgBotCandidateCard(items []OrgBotCandidate) string {
	buttons := make([]orgBotCardButton, 0, orgBotChoiceButtonLimit)
	for _, item := range items {
		if len(buttons) >= orgBotChoiceButtonLimit {
			break
		}
		var action string
		switch item.Kind {
		case orgBotTargetPersonal:
			action = "target:personal"
		case orgBotTargetProject:
			if item.ProjectID == "" {
				continue
			}
			action = "project:" + item.ProjectID
		default:
			if item.UnitID == "" {
				continue
			}
			action = "unit:" + item.UnitID
		}
		buttons = append(buttons, orgBotCardButton{Label: item.display(), Action: action})
	}
	if len(buttons) == 0 {
		return ""
	}
	card, err := orgBotChoiceCard("发给谁", buttons)
	if err != nil {
		return ""
	}
	return card
}

func orgBotProjectCard(buttons []orgBotCardButton) string {
	if len(buttons) > orgBotChoiceButtonLimit {
		buttons = buttons[:orgBotChoiceButtonLimit]
	}
	card, err := orgBotChoiceCard("选择项目", buttons)
	if err != nil {
		return ""
	}
	return card
}
