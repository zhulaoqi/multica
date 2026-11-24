package lark

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestOrgBotFrameDecoderCardAction(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","app_id":"cli_x"},"event":{"operator":{"open_id":"ou_user"},"action":{"value":{"action":"target:personal"}},"context":{"open_chat_id":"oc_dm"}}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotCardActionType || msg.CommandBody != "target:personal" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.ChatID != "oc_dm" || msg.SenderOpenID != "ou_user" || msg.MessageID != "ev_1" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOrgBotFrameDecoderMenuClick(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_menu","event_type":"application.bot.menu_v6","app_id":"cli_x"},"event":{"operator":{"operator_id":{"open_id":"ou_user"}},"event_key":"s","timestamp":1669364458}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotMenuActionType || msg.CommandBody != "s" || msg.ChatID != "" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.SenderOpenID != "ou_user" || msg.MessageID != "ev_menu" || msg.ChatType != ChatTypeP2P {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOrgBotFrameDecoderChatEntered(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_enter","event_type":"im.chat.access_event.bot_p2p_chat_entered_v1","app_id":"cli_x"},"event":{"chat_id":"oc_dm","operator_id":{"open_id":"ou_user"}}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotChatEnteredType || msg.ChatID != "oc_dm" || msg.SenderOpenID != "ou_user" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOrgBotCardsStayCompact(t *testing.T) {
	choice, err := orgBotChoiceCard("发给谁", []orgBotCardButton{{Label: "我的小队", Action: "target:personal"}})
	if err != nil {
		t.Fatalf("choice: %v", err)
	}
	selected, err := orgBotSelectedCard(OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectName: "AIHUB",
		UnitPath:    "企业技术中心/研发部/开发二组",
		ProjectID:   pgtype.UUID{Valid: true},
	}, true)
	if err != nil {
		t.Fatalf("selected: %v", err)
	}
	dispatched, err := orgBotDispatchedCard("项目 AIHUB", "把登录失败的提示改清楚", "AIHU-6", "企业技术中心/研发部/开发二组", "https://yiyan.example/aihub/issues/AIHU-6", "", true)
	if err != nil {
		t.Fatalf("dispatched: %v", err)
	}
	start, err := OrgDispatchStartCard("AIHU-6", "把登录失败的提示改清楚", "开发二组", "https://yiyan.example/aihub/issues/AIHU-6")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, card := range []string{choice, selected, dispatched, start} {
		if strings.Contains(card, `"subtitle"`) || strings.Contains(card, "队长会拆解") {
			t.Fatalf("card is not compact: %s", card)
		}
	}
	if !strings.Contains(choice, `"target:personal"`) || !strings.Contains(selected, "已选中 项目 AIHUB") {
		t.Fatalf("choice=%s selected=%s", choice, selected)
	}
	if !strings.Contains(dispatched, "AIHU-6") || !strings.Contains(dispatched, "把登录失败的提示改清楚") || strings.Contains(dispatched, "ECLI-") {
		t.Fatalf("dispatched=%s", dispatched)
	}
	if !strings.Contains(start, "部门开始处理") || !strings.Contains(start, "AIHU-6") {
		t.Fatalf("start=%s", start)
	}
}

func TestLarkJSONFrameDecoderStillDropsCardAction(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","app_id":"cli_x"},"event":{"operator":{"open_id":"ou_user"},"action":{"value":{"action":"target:personal"}},"context":{"open_chat_id":"oc_dm"}}}`)
	_, ok, err := NewLarkJSONFrameDecoder().Decode(raw, Installation{})
	if err != nil || ok {
		t.Fatalf("shared decoder accepted a card action: ok %v err %v", ok, err)
	}
}
