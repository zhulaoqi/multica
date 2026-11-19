package main

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDispatchOriginReplyTarget(t *testing.T) {
	cases := []struct {
		name   string
		origin db.DispatchOrigin
		want   string
		thread bool
	}{
		{
			name: "topic reply",
			origin: db.DispatchOrigin{
				OriginMessageID: pgtype.Text{String: "om_1", Valid: true},
				ThreadID:        pgtype.Text{String: "omt_1", Valid: true},
			},
			want:   "om_1",
			thread: true,
		},
		{
			name: "group message without topic replies to the message only",
			origin: db.DispatchOrigin{
				OriginMessageID: pgtype.Text{String: "om_2", Valid: true},
			},
			want: "om_2",
		},
		{
			name:   "legacy origin stays chat-level",
			origin: db.DispatchOrigin{},
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := dispatchOriginReplyTarget(tc.origin)
			if target.MessageID != tc.want || target.InThread != tc.thread {
				t.Fatalf("target = %+v, want message %q thread %v", target, tc.want, tc.thread)
			}
		})
	}
}

func TestDispatchStartTextNamesDepartmentAndLink(t *testing.T) {
	notice := &service.ChatDispatchStartNotice{
		Issue:      db.Issue{Number: 7, Title: "对账"},
		UnitName:   "支付组",
		Identifier: "PAY-7",
	}
	text := dispatchStartText(notice, "pay", "https://app.example.com")
	for _, want := range []string{"开始处理", "PAY-7", "对账", "支付组", "https://app.example.com/pay/issues/PAY-7"} {
		if !strings.Contains(text, want) {
			t.Fatalf("start text missing %q: %q", want, text)
		}
	}
}
