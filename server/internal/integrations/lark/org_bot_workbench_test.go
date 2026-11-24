package lark

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type memOrgBotContext struct {
	state OrgBotChatContext
	ok    bool
}

func (m *memOrgBotContext) Load(context.Context, pgtype.UUID, string, pgtype.UUID) (OrgBotChatContext, error) {
	if !m.ok {
		return OrgBotChatContext{}, ErrOrgBotContextMissing
	}
	return m.state, nil
}

func (m *memOrgBotContext) Save(_ context.Context, _ pgtype.UUID, _ string, _ pgtype.UUID, state OrgBotChatContext) error {
	m.state = state
	m.ok = true
	return nil
}

type orgBotProjectsFake struct {
	projects []service.RequestableProject
}

func (f orgBotProjectsFake) ListRequestableProjects(context.Context, pgtype.UUID, pgtype.UUID) ([]service.RequestableProject, error) {
	return f.projects, nil
}

func payRoute() *orgBotRouterFake {
	return &orgBotRouterFake{result: service.RouteResult{
		Path: "财务/支付组",
		Unit: db.ListOrgUnitsRow{ID: testUUID(0x21), Name: "支付组"},
	}}
}

func createdPayDispatch(title string) service.OrgDispatchResult {
	return service.OrgDispatchResult{
		Issue: db.Issue{ID: testUUID(0x42), Number: 7, Title: title},
		Unit:  db.ListOrgUnitsRow{ID: testUUID(0x21), Name: "支付组"},
	}
}

func p2pText(id, body string) InboundMessage {
	return InboundMessage{
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    id,
		SenderOpenID: "ou_user",
		MessageType:  "text",
		CommandBody:  body,
	}
}

func TestOrgBot_RememberedDepartmentUsedOnNextMessage(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("看一下对账差异")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	router := payRoute()
	h.bot.cfg.Router = router
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "帮支付组看一下对账差异")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "再看一眼退款")); err != nil {
		t.Fatalf("second: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("route calls = %d, want 1", router.calls)
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("dispatch calls = %d", len(dispatcher.calls))
	}
	if dispatcher.calls[1].Description != "再看一眼退款" || dispatcher.calls[1].UnitID != testUUID(0x21) {
		t.Fatalf("second dispatch = %+v", dispatcher.calls[1])
	}
	if !strings.Contains(h.client.textSent[1].Text, "当前发给 财务/支付组") {
		t.Fatalf("reply = %q", h.client.textSent[1].Text)
	}
}

func TestOrgBot_AmbiguousChoiceDispatchesOriginalSentence(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("账单")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	router := &orgBotRouterFake{
		err: service.ErrDispatchAmbiguous,
		result: service.RouteResult{Candidates: []service.RouteCandidate{
			{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
			{UnitID: util.UUIDToString(testUUID(0x22)), Path: "财务/结算组"},
		}},
	}
	h.bot.cfg.Router = router
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "帮我看一下账单")); err != nil {
		t.Fatalf("first: %v", err)
	}
	res, err := h.bot.Handle(context.Background(), p2pText("m2", "1"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Outcome != OutcomeDispatchCreated {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if router.calls != 1 {
		t.Fatalf("route calls = %d, want 1", router.calls)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].Description != "帮我看一下账单" || dispatcher.calls[0].UnitID != testUUID(0x21) {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
}

func TestOrgBot_PersonalTargetIsRemembered(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("纪要")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "我的小队")); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("choosing a squad dispatched")
	}
	if !strings.Contains(h.client.textSent[0].Text, "我的小队") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "整理本周纪要")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || !dispatcher.calls[0].Personal || dispatcher.calls[0].Description != "整理本周纪要" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	if !strings.Contains(h.client.textSent[1].Text, "当前发给 我的小队") {
		t.Fatalf("reply = %q", h.client.textSent[1].Text)
	}
}

func TestOrgBot_ProjectTargetIsRemembered(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("登录")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "支付中台", Path: "财务/支付组",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "项目")); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(h.client.textSent[0].Text, "支付中台") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "支付中台")); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("picking a project dispatched")
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m3", "修一下登录")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if h.bot.cfg.Router.(*orgBotRouterFake).calls != 0 {
		t.Fatalf("project target was routed")
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ProjectID != testUUID(0x31) || dispatcher.calls[0].Description != "修一下登录" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
}

func TestOrgBot_AddressedGroupPlainTextDispatches(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("帮支付组看一下对账差异"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchCreated || len(dispatcher.calls) != 1 {
		t.Fatalf("outcome = %q calls = %d", res.Outcome, len(dispatcher.calls))
	}
}

func TestOrgBot_StagedFileAttachesToCreatedTask(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{}
	h.client.resource = DownloadedResource{Data: []byte("png-bytes"), ContentType: "image/png", Filename: "shot.png"}

	img := p2pText("img", "")
	img.MessageType = "image"
	img.Content = `{"image_key":"img_1"}`
	if _, err := h.bot.Handle(context.Background(), img); err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(h.attachments.attachments) != 0 {
		t.Fatalf("attached before the task existed")
	}
	if !strings.Contains(h.client.textSent[0].Text, "已收到") {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "帮支付组看一下对账差异")); err != nil {
		t.Fatalf("text: %v", err)
	}
	if len(h.attachments.attachments) != 1 {
		t.Fatalf("attachments = %d", len(h.attachments.attachments))
	}
	att := h.attachments.attachments[0]
	if att.IssueID != testUUID(0x42) || att.WorkspaceID != testUUID(0x01) || string(att.Data) != "png-bytes" {
		t.Fatalf("attachment = %+v", att)
	}
	if !strings.Contains(h.client.textSent[1].Text, "已附上") {
		t.Fatalf("reply = %q", h.client.textSent[1].Text)
	}
}

func TestOrgBot_FileAfterDispatchAttachesToThatTask(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{}
	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "帮支付组看一下对账差异")); err != nil {
		t.Fatalf("text: %v", err)
	}
	h.client.resource = DownloadedResource{Data: []byte("png-bytes"), ContentType: "image/png"}
	img := p2pText("img", "")
	img.MessageType = "image"
	img.Content = `{"image_key":"img_1"}`
	if _, err := h.bot.Handle(context.Background(), img); err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(h.attachments.attachments) != 1 || h.attachments.attachments[0].IssueID != testUUID(0x42) {
		t.Fatalf("attachment = %+v", h.attachments.attachments)
	}
	if !strings.Contains(h.client.textSent[1].Text, "刚才的 ORG-7") {
		t.Fatalf("reply = %q", h.client.textSent[1].Text)
	}
}

func TestOrgBot_CardActionSelectsPersonalTarget(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("纪要")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{}

	click := p2pText("c1", "target:personal")
	click.MessageType = orgBotCardActionType
	if _, err := h.bot.Handle(context.Background(), click); err != nil {
		t.Fatalf("click: %v", err)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "整理本周纪要")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || !dispatcher.calls[0].Personal {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
}
