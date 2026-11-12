package main

import (
	"context"
	"strings"
	"testing"
)

func TestResolveFeishuInboxSkipsNonMembersAndIntakeCards(t *testing.T) {
	_, ok := resolveFeishuInboxTarget(context.Background(), nil, nil, "app", "secret", map[string]any{
		"recipient_type": "agent",
		"type":           "new_comment",
	})
	if ok {
		t.Fatal("agents do not receive Feishu inbox messages")
	}
	_, ok = resolveFeishuInboxTarget(context.Background(), nil, nil, "app", "secret", map[string]any{
		"recipient_type": "member",
		"type":           "intake_blocker",
		"recipient_id":   "00000000-0000-0000-0000-000000000001",
		"workspace_id":   "00000000-0000-0000-0000-000000000002",
	})
	if ok {
		t.Fatal("intake blocker already has its own Feishu card")
	}
}

func TestFeishuInboxCardUsesChineseLabel(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	card, err := feishuInboxCard(context.Background(), nil, map[string]any{
		"type":     "mentioned",
		"title":    "看一下这个任务",
		"body":     "这里需要你确认",
		"severity": "action_required",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"有人提到你", "看一下这个任务", "这里需要你确认", "https://app.example.com/inbox"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card missing %q:\n%s", want, card)
		}
	}
}
