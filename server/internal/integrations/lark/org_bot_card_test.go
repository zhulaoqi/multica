package lark

import "testing"

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

func TestLarkJSONFrameDecoderStillDropsCardAction(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","app_id":"cli_x"},"event":{"operator":{"open_id":"ou_user"},"action":{"value":{"action":"target:personal"}},"context":{"open_chat_id":"oc_dm"}}}`)
	_, ok, err := NewLarkJSONFrameDecoder().Decode(raw, Installation{})
	if err != nil || ok {
		t.Fatalf("shared decoder accepted a card action: ok %v err %v", ok, err)
	}
}
