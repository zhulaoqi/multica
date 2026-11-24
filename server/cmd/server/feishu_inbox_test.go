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

func TestFeishuInboxCardStatesTheNewStatusAndAssignee(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	status, err := feishuInboxCard(context.Background(), nil, map[string]any{
		"type":     "status_changed",
		"title":    "我想换项目",
		"issue_id": "00000000-0000-0000-0000-000000000009",
		"details": map[string]string{
			"from": "todo",
			"to":   "in_progress",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"状态已更新", "我想换项目", "待处理 → 处理中", "打开任务"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status card missing %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "状态变了") {
		t.Fatalf("status card still uses the old title:\n%s", status)
	}

	assignee, err := feishuInboxCard(context.Background(), nil, map[string]any{
		"type":     "assignee_changed",
		"title":    "我想换项目",
		"issue_id": "00000000-0000-0000-0000-000000000009",
		"details": map[string]any{
			"prev_assignee_type": "member",
			"prev_assignee_id":   "00000000-0000-0000-0000-000000000001",
			"new_assignee_type":  "agent",
			"new_assignee_id":    "00000000-0000-0000-0000-000000000002",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(assignee, "负责人已更新") || strings.Contains(assignee, "负责人变了") {
		t.Fatalf("assignee title = %s", assignee)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
		if strings.Contains(assignee, id) {
			t.Fatalf("assignee card printed a raw id:\n%s", assignee)
		}
	}
}
