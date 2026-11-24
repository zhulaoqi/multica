package lark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		ChatID     string `json:"chat_id"`
		OperatorID struct {
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
		EventType:      env.Header.EventType,
		EventID:        env.Header.EventID,
		AppID:          env.Header.AppID,
		ChatID:         ChatID(chatID),
		ChatType:       ChatTypeP2P,
		MessageID:      env.Header.EventID,
		SenderOpenID:   OpenID(openID),
		MessageType:    orgBotChatEnteredType,
		AddressedToBot: true,
		CreateTime:     env.Header.CreateTime,
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
func orgBotChoiceCard(title string, buttons []orgBotCardButton) (string, error) {
	return orgBotNoticeCard("blue", title, nil, buttons, "", "")
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

// OrgDispatchStartCard is the notice sent when a department begins the work.
func OrgDispatchStartCard(identifier, title, unit, link string) (string, error) {
	return orgBotNoticeCard("blue", "部门开始处理", []orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
		{Label: "部门", Value: unit},
	}, orgBotSwitchButtons(true), "打开任务", link)
}

// OrgDispatchDoneCard is the notice sent when that work is finished.
func OrgDispatchDoneCard(identifier, title, status, link string) (string, error) {
	return orgBotNoticeCard("green", "已完成", []orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
		{Label: "状态", Value: orgBotStatusLabel(status)},
	}, orgBotSwitchButtons(true), "打开任务", link)
}

// OrgDispatchFailedCard is the notice sent when the department could not run it.
func OrgDispatchFailedCard(identifier, title, unit, reason, link string) (string, error) {
	return orgBotNoticeCard("red", "派活失败", []orgBotCardField{
		{Label: "需求", Value: title},
		{Label: "编号", Value: identifier},
		{Label: "部门", Value: unit},
		{Label: "原因", Value: reason},
	}, orgBotSwitchButtons(true), "打开任务", link)
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

func orgBotNoticeCard(template, title string, fields []orgBotCardField, buttons []orgBotCardButton, openLabel, openURL string) (string, error) {
	elements := make([]any, 0, len(fields)+len(buttons)+1)
	for _, field := range fields {
		value := cardPlain(field.Value)
		if value == "" {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":                "column_set",
			"flex_mode":          "none",
			"background_style":   "default",
			"horizontal_spacing": "8px",
			"columns": []any{
				map[string]any{
					"tag":            "column",
					"width":          "weighted",
					"weight":         1,
					"vertical_align": "center",
					"elements": []any{
						map[string]any{"tag": "markdown", "content": "**" + field.Label + "**"},
					},
				},
				map[string]any{
					"tag":            "column",
					"width":          "weighted",
					"weight":         3,
					"vertical_align": "center",
					"elements": []any{
						map[string]any{"tag": "markdown", "content": value},
					},
				},
			},
		})
	}
	for i, button := range buttons {
		kind := "default"
		if i == 0 && strings.TrimSpace(openURL) == "" {
			kind = "primary"
		}
		action := map[string]string{"action": button.Action}
		elements = append(elements, map[string]any{
			"tag":   "button",
			"text":  map[string]any{"tag": "plain_text", "content": trimCardButton(button.Label)},
			"type":  kind,
			"value": action,
			"behaviors": []any{
				map[string]any{"type": "callback", "value": action},
			},
		})
	}
	if url := strings.TrimSpace(openURL); url != "" {
		label := strings.TrimSpace(openLabel)
		if label == "" {
			label = "打开"
		}
		elements = append(elements, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": label},
			"type": "primary",
			"behaviors": []any{
				map[string]any{"type": "open_url", "default_url": url},
			},
		})
	}
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
		"body": map[string]any{"elements": elements},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
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
