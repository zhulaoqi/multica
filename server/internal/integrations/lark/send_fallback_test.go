package lark

import (
	"context"
	"log/slog"
	"testing"
)

func TestSendTextWithReplyFallback_ClassifiedThreadErrorRetriesAtChatLevel(t *testing.T) {
	api := &fakeAPIClient{threadReplyErr: errThreadReplyClassified}

	err := SendTextWithReplyFallback(context.Background(), slog.New(slog.DiscardHandler), "test send", api, SendTextParams{
		ChatID:      "oc_chat",
		Text:        "开始处理",
		ReplyTarget: ReplyTarget{MessageID: "om_parent", InThread: true},
	})
	if err != nil {
		t.Fatalf("SendTextWithReplyFallback: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 2 {
		t.Fatalf("sends = %d, want topic attempt + chat fallback", len(api.textSent))
	}
	if !api.textSent[0].ReplyTarget.IsSet() || api.textSent[0].ReplyTarget.InThread != true {
		t.Fatalf("first send lost the thread target: %+v", api.textSent[0].ReplyTarget)
	}
	if api.textSent[1].ReplyTarget.IsSet() {
		t.Fatalf("fallback must be chat-level: %+v", api.textSent[1].ReplyTarget)
	}
}

func TestSendTextWithReplyFallback_AmbiguousErrorDoesNotDuplicate(t *testing.T) {
	api := &fakeAPIClient{threadReplyErr: errThreadReplyTransport}

	err := SendTextWithReplyFallback(context.Background(), slog.New(slog.DiscardHandler), "test send", api, SendTextParams{
		ChatID:      "oc_chat",
		Text:        "开始处理",
		ReplyTarget: ReplyTarget{MessageID: "om_parent"},
	})
	if err == nil {
		t.Fatal("want the ambiguous failure surfaced")
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 {
		t.Fatalf("sends = %d, want no chat-level retry", len(api.textSent))
	}
}
