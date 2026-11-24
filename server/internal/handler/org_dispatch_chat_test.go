package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedChatDispatch runs one dispatch through the same service the chat bot
// uses, so the control-surface tests exercise real origin rows, squad leaders
// and issue statuses.
func seedChatDispatch(t *testing.T, requester, unitName, title string) orgDispatchResponse {
	t.Helper()
	leader := insertRunnableDutyAgent(t, unitName+"-leader", requester)
	unitID := createOrgUnitAs(t, requester, unitName, "")
	placeOrgAgent(t, requester, leader, unitID)
	status, res, body := dispatchToOrgUnit(t, requester, unitID, map[string]any{
		"title":       title,
		"description": "## 目标\n聊天控制面测试",
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d: %s", status, body)
	}
	return res
}

func loadIssueForChat(t *testing.T, issueID string) db.Issue {
	t.Helper()
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return issue
}

func TestChatDispatch_ListAndFindOpenDispatch(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-list@example.com")
	res := seedChatDispatch(t, requester, "聊天列表组", "dispatch-test 聊天列表")

	list, err := testHandler.OrgDispatchService.ListChatDispatches(context.Background(), parseUUID(requester), 10)
	if err != nil {
		t.Fatalf("ListChatDispatches: %v", err)
	}
	if len(list) != 1 || list[0].IssueID != parseUUID(res.Issue.ID) {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Title != "dispatch-test 聊天列表" || list[0].UnitName != "聊天列表组" {
		t.Fatalf("row = %+v", list[0])
	}
	if list[0].Identifier == "" || !strings.Contains(list[0].Identifier, "-") {
		t.Fatalf("identifier = %q", list[0].Identifier)
	}

	found, err := testHandler.OrgDispatchService.FindChatDispatch(context.Background(), parseUUID(requester), "1")
	if err != nil || found.IssueID != parseUUID(res.Issue.ID) {
		t.Fatalf("find by position: %+v %v", found, err)
	}
	found, err = testHandler.OrgDispatchService.FindChatDispatch(context.Background(), parseUUID(requester), list[0].Identifier)
	if err != nil || found.IssueID != parseUUID(res.Issue.ID) {
		t.Fatalf("find by identifier: %+v %v", found, err)
	}
	if _, err := testHandler.OrgDispatchService.FindChatDispatch(context.Background(), parseUUID(requester), "nope-nope"); !errors.Is(err, service.ErrChatDispatchNotFound) {
		t.Fatalf("unknown reference err = %v", err)
	}
}

func TestChatDispatch_OnlyRequesterCanControl(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-owner@example.com")
	other := createOrgTestAdmin(t, "chat-other@example.com")
	res := seedChatDispatch(t, requester, "聊天权限组", "dispatch-test 聊天权限")

	_, err := testHandler.OrgDispatchService.CancelChatDispatch(context.Background(), parseUUID(other), parseUUID(res.Issue.ID))
	if !errors.Is(err, service.ErrChatDispatchNotOwner) {
		t.Fatalf("cancel by other err = %v, want not owner", err)
	}
	if issue := loadIssueForChat(t, res.Issue.ID); issue.Status == "cancelled" {
		t.Fatal("another member cancelled the dispatch")
	}
}

func TestChatDispatch_AcceptClosesDispatch(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-accept@example.com")
	res := seedChatDispatch(t, requester, "聊天验收组", "dispatch-test 聊天验收")

	updated, err := testHandler.OrgDispatchService.AcceptChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if updated.Status != "done" {
		t.Fatalf("status = %q, want done", updated.Status)
	}
	if _, err := testHandler.OrgDispatchService.AcceptChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID)); !errors.Is(err, service.ErrChatDispatchClosed) {
		t.Fatalf("second accept err = %v", err)
	}
}

func TestChatDispatch_CancelStopsWork(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-cancel@example.com")
	res := seedChatDispatch(t, requester, "聊天取消组", "dispatch-test 聊天取消")

	updated, err := testHandler.OrgDispatchService.CancelChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", updated.Status)
	}
	if _, err := testHandler.OrgDispatchService.CancelChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID)); !errors.Is(err, service.ErrChatDispatchClosed) {
		t.Fatalf("second cancel err = %v", err)
	}
	if _, err := testHandler.OrgDispatchService.RejectChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID), "x"); !errors.Is(err, service.ErrChatDispatchClosed) {
		t.Fatalf("reject after cancel err = %v", err)
	}
}

func TestChatDispatch_RejectRecordsReasonAndReopens(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-reject@example.com")
	res := seedChatDispatch(t, requester, "聊天驳回组", "dispatch-test 聊天驳回")

	reply, err := testHandler.OrgDispatchService.RejectChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID), "金额口径不对")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if reply.Issue.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", reply.Issue.Status)
	}
	if !strings.Contains(reply.Comment.Content, "验收未通过：金额口径不对") {
		t.Fatalf("comment = %q", reply.Comment.Content)
	}
}

func TestChatDispatch_FollowUpCommentsWithoutChangingStatus(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-follow@example.com")
	res := seedChatDispatch(t, requester, "聊天追问组", "dispatch-test 聊天追问")
	before := loadIssueForChat(t, res.Issue.ID)

	reply, err := testHandler.OrgDispatchService.FollowUpChatDispatch(context.Background(), parseUUID(requester), parseUUID(res.Issue.ID), "补充一下汇率口径")
	if err != nil {
		t.Fatalf("follow up: %v", err)
	}
	if reply.Comment.Content != "补充一下汇率口径" {
		t.Fatalf("comment = %q", reply.Comment.Content)
	}
	after := loadIssueForChat(t, res.Issue.ID)
	if after.Status != before.Status {
		t.Fatalf("status changed on follow-up: %q -> %q", before.Status, after.Status)
	}
}

func TestChatDispatch_ResolvesByThread(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-thread@example.com")
	other := createOrgTestAdmin(t, "chat-thread-other@example.com")
	res := seedChatDispatch(t, requester, "聊天话题组", "dispatch-test 聊天话题")

	if _, err := testPool.Exec(context.Background(), `
		UPDATE dispatch_origin SET chat_id = 'oc_chat', thread_id = 'omt_thread', origin_message_id = 'om_root'
		WHERE issue_id = $1
	`, res.Issue.ID); err != nil {
		t.Fatalf("mark origin thread: %v", err)
	}

	found, err := testHandler.OrgDispatchService.GetChatDispatchByThread(context.Background(), parseUUID(requester), "oc_chat", "omt_thread")
	if err != nil || found.IssueID != parseUUID(res.Issue.ID) {
		t.Fatalf("thread lookup: %+v %v", found, err)
	}
	if _, err := testHandler.OrgDispatchService.GetChatDispatchByThread(context.Background(), parseUUID(other), "oc_chat", "omt_thread"); !errors.Is(err, service.ErrChatDispatchNotFound) {
		t.Fatalf("other member thread lookup err = %v", err)
	}
	if _, err := testHandler.OrgDispatchService.GetChatDispatchByThread(context.Background(), parseUUID(requester), "oc_chat", "omt_other"); !errors.Is(err, service.ErrChatDispatchNotFound) {
		t.Fatalf("unknown thread err = %v", err)
	}
}

func TestChatDispatch_StartNoticeClaimsOnce(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "chat-start@example.com")
	res := seedChatDispatch(t, requester, "聊天开始组", "dispatch-test 聊天开始")
	issue := loadIssueForChat(t, res.Issue.ID)

	if _, err := testPool.Exec(context.Background(), `
		UPDATE dispatch_origin SET channel_type = 'feishu', chat_id = 'oc_chat', thread_id = 'omt_thread',
			origin_message_id = 'om_root'
		WHERE issue_id = $1
	`, res.Issue.ID); err != nil {
		t.Fatalf("mark origin feishu: %v", err)
	}

	notice, err := testHandler.OrgDispatchService.StartDispatch(context.Background(), issue)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if notice == nil {
		t.Fatal("first start produced no notice")
	}
	if notice.Identifier == "" {
		t.Fatalf("notice = %+v", notice)
	}
	if notice.Origin.OriginMessageID.String != "om_root" || notice.Origin.ThreadID.String != "omt_thread" {
		t.Fatalf("origin coordinates lost: %+v", notice.Origin)
	}
	if again, err := testHandler.OrgDispatchService.StartDispatch(context.Background(), issue); err != nil || again != nil {
		t.Fatalf("second start = %+v, %v; want silence", again, err)
	}
}
