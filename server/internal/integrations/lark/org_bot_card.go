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
// only, card button clicks. Per-agent bots keep the message decoder, which
// still ignores card.action.trigger.
func NewOrgBotFrameDecoder() FrameDecoder { return orgBotFrameDecoder{} }

type orgBotFrameDecoder struct{}

func (orgBotFrameDecoder) Decode(payload []byte, inst Installation) (InboundMessage, bool, error) {
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(payload, inst)
	if err != nil || ok {
		return msg, ok, err
	}
	return decodeOrgBotCardAction(payload)
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

func orgBotChoiceCard(title, body string, buttons []orgBotCardButton) (string, error) {
	if len(buttons) == 0 {
		return "", nil
	}
	actions := make([]any, 0, len(buttons))
	for i, button := range buttons {
		kind := "default"
		if i == 0 {
			kind = "primary"
		}
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": trimCardButton(button.Label)},
			"type": kind,
			"value": map[string]string{
				"action": button.Action,
			},
		})
	}
	doc := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "blue",
			"title":    map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": []any{
			map[string]any{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": body},
			},
			map[string]any{
				"tag":     "action",
				"actions": actions,
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
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

func orgBotMenuCard() string {
	card, err := orgBotChoiceCard("发给谁", "选择之后的消息发给谁。", []orgBotCardButton{
		{Label: "部门", Action: "target:department"},
		{Label: "我的小队", Action: "target:personal"},
		{Label: "项目", Action: "target:project"},
	})
	if err != nil {
		return ""
	}
	return card
}

func orgBotCandidateCard(items []OrgBotCandidate) string {
	buttons := make([]orgBotCardButton, 0, orgBotChoiceButtonLimit)
	for _, item := range items {
		if item.UnitID == "" {
			continue
		}
		if len(buttons) >= orgBotChoiceButtonLimit {
			break
		}
		label := item.Path
		if label == "" {
			label = item.Name
		}
		buttons = append(buttons, orgBotCardButton{Label: label, Action: "unit:" + item.UnitID})
	}
	if len(buttons) == 0 {
		return ""
	}
	card, err := orgBotChoiceCard("选择部门", "点一个部门，或回复序号。", buttons)
	if err != nil {
		return ""
	}
	return card
}

func orgBotProjectCard(buttons []orgBotCardButton) string {
	if len(buttons) > orgBotChoiceButtonLimit {
		buttons = buttons[:orgBotChoiceButtonLimit]
	}
	card, err := orgBotChoiceCard("选择项目", "点一个项目，或回复项目名称。", buttons)
	if err != nil {
		return ""
	}
	return card
}
