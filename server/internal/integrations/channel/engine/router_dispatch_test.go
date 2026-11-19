package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeDispatcher struct {
	matches     []service.UnitMatch
	resolveErr  error
	result      service.OrgDispatchResult
	dispatchErr error
	lastParams  service.OrgDispatchParams
	calls       int
}

func (f *fakeDispatcher) ResolveUnits(_ context.Context, _, _ pgtype.UUID, _ string) ([]service.UnitMatch, error) {
	return f.matches, f.resolveErr
}

func (f *fakeDispatcher) Dispatch(_ context.Context, p service.OrgDispatchParams) (service.OrgDispatchResult, error) {
	f.calls++
	f.lastParams = p
	return f.result, f.dispatchErr
}

func dispatchHarness(t *testing.T, cmd *DispatchCommand, dispatcher *fakeDispatcher) *harness {
	t.Helper()
	h := newHarness(t)
	h.router.dispatcher = dispatcher
	h.binder.appendResult.DispatchCommand = cmd
	return h
}

func TestRouter_DispatchCommand_CreatesDepartmentDispatch(t *testing.T) {
	unitID := uuidFromString(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	issueID := uuidFromString(t, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	dispatcher := &fakeDispatcher{
		matches: []service.UnitMatch{{
			Unit: db.ListOrgUnitsRow{ID: unitID, Name: "支付组"},
			Path: "研发中心/支付组",
		}},
		result: service.OrgDispatchResult{
			Issue: db.Issue{ID: issueID, Number: 42, Title: "支付回调改造"},
			Unit:  db.ListOrgUnitsRow{ID: unitID, Name: "支付组"},
		},
	}
	cmd := &DispatchCommand{Unit: "支付组", Title: "支付回调改造", Goal: "支持新版回调", Acceptance: "回调成功"}
	h := dispatchHarness(t, cmd, dispatcher)

	msg := p2pMessage(t)
	msg.CommandText = "/dispatch 支付组\n支付回调改造\n目标: 支持新版回调\n验收: 回调成功"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if h.tasks.calls() != 0 {
		t.Fatalf("dispatch enqueued a chat run, calls=%d", h.tasks.calls())
	}
	if h.issues.called {
		t.Fatal("dispatch created an issue in the router instead of the service")
	}
	if dispatcher.calls != 1 {
		t.Fatalf("Dispatch() calls = %d, want 1", dispatcher.calls)
	}
	params := dispatcher.lastParams
	if params.ActorID != h.ident.id.UserID {
		t.Fatalf("actor = %v, want sender %v", params.ActorID, h.ident.id.UserID)
	}
	if params.Origin.ChannelType != string(channel.TypeFeishu) || params.Origin.ChatID != "oc_chat" {
		t.Fatalf("origin = %+v, want the feishu chat", params.Origin)
	}
	if params.Description != "## 目标\n支持新版回调\n\n## 验收\n回调成功" {
		t.Fatalf("description = %q", params.Description)
	}
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		return len(calls) == 1 &&
			calls[0].Outcome == OutcomeDispatchCreated &&
			calls[0].IssueIdentifier == "MUL-42" &&
			calls[0].DispatchUnitName == "支付组"
	}) {
		t.Fatalf("dispatch confirmation not delivered: %+v", h.replier.calls())
	}
}

func TestRouter_DispatchCommand_AmbiguousDepartmentListsCandidates(t *testing.T) {
	dispatcher := &fakeDispatcher{matches: []service.UnitMatch{
		{Unit: db.ListOrgUnitsRow{Name: "支付组"}, Path: "研发中心/支付组"},
		{Unit: db.ListOrgUnitsRow{Name: "支付组"}, Path: "海外中心/支付组"},
	}}
	h := dispatchHarness(t, &DispatchCommand{Unit: "支付组", Title: "t", Goal: "g", Acceptance: "a"}, dispatcher)

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("ambiguous dispatch called Dispatch() %d times", dispatcher.calls)
	}
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		return len(calls) == 1 &&
			calls[0].Outcome == OutcomeDispatchAmbiguous &&
			len(calls[0].DispatchCandidates) == 2
	}) {
		t.Fatalf("ambiguous result not delivered: %+v", h.replier.calls())
	}
}

func TestRouter_DispatchCommand_InvalidIsUsageOnly(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := dispatchHarness(t, &DispatchCommand{Unit: "支付组", Title: "t", Goal: "g"}, dispatcher)

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("invalid dispatch called Dispatch() %d times", dispatcher.calls)
	}
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		return len(calls) == 1 && calls[0].Outcome == OutcomeDispatchUsage
	}) {
		t.Fatalf("dispatch usage result not delivered: %+v", h.replier.calls())
	}
}

func TestRouter_DispatchCommand_MapsServiceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		outcome Outcome
	}{
		{"no agent", service.ErrOrgUnitNoAgents, OutcomeDispatchNoAgents},
		{"requester unbound", service.ErrDispatchRequesterUnbound, OutcomeDispatchUnbound},
		{"leader unavailable", service.ErrDispatchLeaderUnavailable, OutcomeDispatchLeaderUnavailable},
		{"no access", service.ErrOrgUnitNoAccess, OutcomeDispatchNoAccess},
		{"not a member", service.ErrDispatchRequesterNotInTeam, OutcomeDispatchNoAccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher := &fakeDispatcher{
				matches:     []service.UnitMatch{{Unit: db.ListOrgUnitsRow{Name: "支付组"}, Path: "支付组"}},
				dispatchErr: tc.err,
			}
			h := dispatchHarness(t, &DispatchCommand{Unit: "支付组", Title: "t", Goal: "g", Acceptance: "a"}, dispatcher)
			if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if !waitFor(time.Second, func() bool {
				calls := h.replier.calls()
				return len(calls) == 1 && calls[0].Outcome == tc.outcome
			}) {
				t.Fatalf("result %q not delivered: %+v", tc.outcome, h.replier.calls())
			}
		})
	}
}

func TestRouter_DispatchCommand_UnmatchedDepartmentIsNoAccess(t *testing.T) {
	dispatcher := &fakeDispatcher{matches: nil}
	h := dispatchHarness(t, &DispatchCommand{Unit: "不存在", Title: "t", Goal: "g", Acceptance: "a"}, dispatcher)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		return len(calls) == 1 && calls[0].Outcome == OutcomeDispatchNoAccess
	}) {
		t.Fatalf("no-access result not delivered: %+v", h.replier.calls())
	}
}
