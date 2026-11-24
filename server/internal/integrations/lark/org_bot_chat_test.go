package lark

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type orgBotChatFake struct {
	dispatches []service.ChatDispatch
	listErr    error

	findDispatch  service.ChatDispatch
	findErr       error
	lastReference string

	threadDispatch service.ChatDispatch
	threadErr      error

	cancelErr    error
	acceptErr    error
	rejectErr    error
	followUpErr  error
	rejectReason string
	followUpText string
	delivered    bool

	units        []service.UnitMatch
	resolveErr   error
	reassign     service.OrgDispatchResult
	reassignErr  error
	reassignArgs []service.OrgDispatchReassignParams
	cancelled    []pgtype.UUID
	accepted     []pgtype.UUID
}

func (f *orgBotChatFake) ListChatDispatches(context.Context, pgtype.UUID, int32) ([]service.ChatDispatch, error) {
	return f.dispatches, f.listErr
}

func (f *orgBotChatFake) FindChatDispatch(_ context.Context, _ pgtype.UUID, reference string) (service.ChatDispatch, error) {
	f.lastReference = reference
	if f.findErr != nil {
		return service.ChatDispatch{}, f.findErr
	}
	return f.findDispatch, nil
}

func (f *orgBotChatFake) GetChatDispatchByThread(context.Context, pgtype.UUID, string, string) (service.ChatDispatch, error) {
	return f.threadDispatch, f.threadErr
}

func (f *orgBotChatFake) CancelChatDispatch(_ context.Context, _ pgtype.UUID, issueID pgtype.UUID) (db.Issue, error) {
	f.cancelled = append(f.cancelled, issueID)
	return db.Issue{}, f.cancelErr
}

func (f *orgBotChatFake) AcceptChatDispatch(_ context.Context, _ pgtype.UUID, issueID pgtype.UUID) (db.Issue, error) {
	f.accepted = append(f.accepted, issueID)
	return db.Issue{}, f.acceptErr
}

func (f *orgBotChatFake) RejectChatDispatch(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, reason string) (service.ChatDispatchReply, error) {
	f.rejectReason = reason
	return service.ChatDispatchReply{Delivered: f.delivered}, f.rejectErr
}

func (f *orgBotChatFake) FollowUpChatDispatch(_ context.Context, _ pgtype.UUID, _ pgtype.UUID, text string) (service.ChatDispatchReply, error) {
	f.followUpText = text
	return service.ChatDispatchReply{Delivered: f.delivered}, f.followUpErr
}

func (f *orgBotChatFake) ResolveUnits(context.Context, pgtype.UUID, pgtype.UUID, string) ([]service.UnitMatch, error) {
	return f.units, f.resolveErr
}

func (f *orgBotChatFake) Reassign(_ context.Context, p service.OrgDispatchReassignParams) (service.OrgDispatchResult, error) {
	f.reassignArgs = append(f.reassignArgs, p)
	return f.reassign, f.reassignErr
}

type orgBotAttachmentFake struct {
	attachments []OrgBotAttachment
	err         error
}

func (f *orgBotAttachmentFake) AttachToIssue(_ context.Context, a OrgBotAttachment) (OrgBotAttachmentResult, error) {
	f.attachments = append(f.attachments, a)
	if f.err != nil {
		return OrgBotAttachmentResult{}, f.err
	}
	return OrgBotAttachmentResult{Filename: a.Filename, SizeBytes: int64(len(a.Data))}, nil
}

func chatDispatchFixture() service.ChatDispatch {
	return service.ChatDispatch{
		IssueID:        testUUID(0x51),
		WorkspaceID:    testUUID(0x61),
		Identifier:     "PAY-7",
		Number:         7,
		Title:          "对账",
		Status:         "in_progress",
		StatusCategory: "started",
		LeaderState:    "running",
		UnitName:       "支付组",
		WorkspaceSlug:  "pay",
	}
}

func TestOrgBot_StatusListsOpenDispatches(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.dispatches = []service.ChatDispatch{chatDispatchFixture()}

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/status"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("outcome = %q, want ingested", res.Outcome)
	}
	text := h.client.textSent[0].Text
	for _, want := range []string{"1. PAY-7", "对账", "支付组", "进行中", "https://app.example.com/pay/issues/PAY-7", "/cancel"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status reply missing %q: %q", want, text)
		}
	}
}

func TestOrgBot_StatusWithoutOpenDispatches(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/状态")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := h.client.textSent[0].Text; got != orgBotNoOpenDispatchCopy {
		t.Fatalf("reply = %q", got)
	}
}

func TestOrgBot_CancelByNumber(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/cancel 1")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(h.chat.cancelled) != 1 || h.chat.cancelled[0] != testUUID(0x51) {
		t.Fatalf("cancelled = %+v", h.chat.cancelled)
	}
	if h.chat.lastReference != "1" {
		t.Fatalf("reference = %q, want the position the requester typed", h.chat.lastReference)
	}
	if !strings.Contains(h.client.textSent[0].Text, "已取消 PAY-7") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_CancelClosedDispatchExplains(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()
	h.chat.cancelErr = service.ErrChatDispatchClosed

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/cancel 1")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(h.client.textSent[0].Text, "已经结束") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_RejectPassesReasonAndReportsDelivery(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()
	h.chat.delivered = false

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/驳回 1 金额口径不对")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if h.chat.rejectReason != "金额口径不对" {
		t.Fatalf("reason = %q", h.chat.rejectReason)
	}
	text := h.client.textSent[0].Text
	if !strings.Contains(text, "已退回 PAY-7") || !strings.Contains(text, "队长当前不在线") {
		t.Fatalf("reply = %q", text)
	}
}

func TestOrgBot_AcceptByIdentifier(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/accept PAY-7")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(h.chat.accepted) != 1 || h.chat.accepted[0] != testUUID(0x51) {
		t.Fatalf("accepted = %+v", h.chat.accepted)
	}
	if !strings.Contains(h.client.textSent[0].Text, "已验收 PAY-7") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_ReassignToResolvedDepartment(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()
	h.chat.units = []service.UnitMatch{{Unit: db.ListOrgUnitsRow{ID: testUUID(0x71), Name: "风控组"}, Path: "研发部 / 风控组"}}
	h.chat.reassign = service.OrgDispatchResult{
		Issue: db.Issue{ID: testUUID(0x51), Number: 7, Title: "对账"},
		Unit:  db.ListOrgUnitsRow{Name: "风控组"},
	}

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/reassign 1 风控组"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchCreated {
		t.Fatalf("outcome = %q, want dispatch_created", res.Outcome)
	}
	if len(h.chat.reassignArgs) != 1 {
		t.Fatalf("reassign calls = %d", len(h.chat.reassignArgs))
	}
	call := h.chat.reassignArgs[0]
	if call.IssueID != testUUID(0x51) || call.UnitID != testUUID(0x71) || call.WorkspaceID != testUUID(0x61) {
		t.Fatalf("reassign args = %+v", call)
	}
	if !strings.Contains(h.client.textSent[0].Text, "研发部 / 风控组") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_ReassignRequiresDepartment(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/reassign 1"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage {
		t.Fatalf("outcome = %q, want dispatch_usage", res.Outcome)
	}
	if len(h.chat.reassignArgs) != 0 {
		t.Fatalf("reassigned without a department")
	}
}

func TestOrgBot_ReassignAmbiguousDepartmentListsPaths(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findDispatch = chatDispatchFixture()
	h.chat.units = []service.UnitMatch{
		{Unit: db.ListOrgUnitsRow{ID: testUUID(0x71)}, Path: "研发部 / 支付组"},
		{Unit: db.ListOrgUnitsRow{ID: testUUID(0x72)}, Path: "研发部 / 支付平台组"},
	}

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/reassign 1 支付"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchAmbiguous {
		t.Fatalf("outcome = %q", res.Outcome)
	}
}

func TestOrgBot_UnknownReferenceExplainsOpenDispatches(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.findErr = service.ErrChatDispatchNotFound

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/cancel 9"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchNoAccess {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if got := h.client.textSent[0].Text; got != orgBotNoOpenDispatchCopy {
		t.Fatalf("reply = %q", got)
	}
}

func TestOrgBot_ThreadFollowUpPostsComment(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.threadDispatch = chatDispatchFixture()
	h.chat.delivered = true

	msg := groupDispatchMessage("补充一下账单口径")
	msg.ThreadID = "omt_1"

	res, err := h.bot.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if h.chat.followUpText != "补充一下账单口径" {
		t.Fatalf("follow-up text = %q", h.chat.followUpText)
	}
	text := h.client.textSent[0].Text
	if !strings.Contains(text, "PAY-7") || strings.Contains(text, "不在线") {
		t.Fatalf("reply = %q", text)
	}
}

func TestOrgBot_ThreadFollowUpWithoutDispatchUsesGuidance(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.threadErr = service.ErrChatDispatchNotFound

	msg := groupDispatchMessage("随便说一句")
	msg.ThreadID = "omt_1"

	if _, err := h.bot.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := h.client.textSent[0].Text; got != orgBotNoThreadCopy {
		t.Fatalf("reply = %q", got)
	}
	if h.chat.followUpText != "" {
		t.Fatalf("posted a comment without a thread dispatch")
	}
}

func TestOrgBot_AddressedGroupPlainTextUsesWorkbench(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("你好"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if !strings.Contains(h.client.textSent[0].Text, "直接把要做的事发给我") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_MediaAttachesToThreadDispatch(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.threadDispatch = chatDispatchFixture()
	h.client.resource = DownloadedResource{Data: []byte("png-bytes"), ContentType: "image/png"}

	msg := groupDispatchMessage("")
	msg.ThreadID = "omt_1"
	msg.MessageType = "image"
	msg.Content = `{"image_key":"img_1"}`

	res, err := h.bot.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if len(h.attachments.attachments) != 1 {
		t.Fatalf("attachments = %d", len(h.attachments.attachments))
	}
	att := h.attachments.attachments[0]
	if att.IssueID != testUUID(0x51) || att.WorkspaceID != testUUID(0x61) || att.Filename != "image.png" {
		t.Fatalf("attachment = %+v", att)
	}
	if !strings.Contains(h.client.textSent[0].Text, "PAY-7") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_MediaTooLargeIsRejected(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.chat.threadDispatch = chatDispatchFixture()
	h.client.resource = DownloadedResource{Data: make([]byte, orgBotAttachmentMaxBytes+1)}

	msg := groupDispatchMessage("")
	msg.ThreadID = "omt_1"
	msg.MessageType = "file"
	msg.Content = `{"file_key":"file_1","file_name":"report.pdf"}`

	if _, err := h.bot.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(h.attachments.attachments) != 0 {
		t.Fatalf("stored an oversized attachment")
	}
	if got := h.client.textSent[0].Text; got != orgBotAttachmentTooLargeCopy {
		t.Fatalf("reply = %q", got)
	}
}

func TestOrgBot_ControlCommandRequiresBoundIdentity(t *testing.T) {
	h := newOrgBotHarness(t, orgBotIdentityFake{err: pgx.ErrNoRows}, testOrganization(), &orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/status"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeNeedsBinding {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if len(h.chat.dispatches) != 0 {
		t.Fatalf("listed dispatches for an unbound sender")
	}
}
