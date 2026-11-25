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
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:     orgBotTargetDepartment,
		UnitID:   testUUID(0x21),
		UnitPath: "财务/支付组",
	}}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "帮支付组看一下对账差异")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "再看一眼退款")); err != nil {
		t.Fatalf("second: %v", err)
	}
	if router.calls != 0 {
		t.Fatalf("route calls = %d, want 0", router.calls)
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("dispatch calls = %d", len(dispatcher.calls))
	}
	if dispatcher.calls[0].Description != "帮支付组看一下对账差异" || dispatcher.calls[0].UnitID != testUUID(0x21) {
		t.Fatalf("first dispatch = %+v", dispatcher.calls[0])
	}
	if dispatcher.calls[1].Description != "再看一眼退款" || dispatcher.calls[1].UnitID != testUUID(0x21) {
		t.Fatalf("second dispatch = %+v", dispatcher.calls[1])
	}
	if len(h.client.sent) < 2 || !strings.Contains(h.client.sent[1].CardJSON, "已交给 财务/支付组") || !strings.Contains(h.client.sent[1].CardJSON, "切换") {
		t.Fatalf("card = %+v", h.client.sent)
	}
}

func TestOrgBot_ChoiceKeepsTheSentenceAsTheRequirement(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("账单")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	router := payRoute()
	h.bot.cfg.Router = router
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
		{UnitID: util.UUIDToString(testUUID(0x22)), Path: "财务/结算组"},
	}}
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "帮我看一下账单")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("the sentence was dispatched before a choice")
	}
	res, err := h.bot.Handle(context.Background(), p2pText("m2", "2"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Outcome != OutcomeDispatchCreated {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if router.calls != 0 {
		t.Fatalf("route calls = %d, want 0", router.calls)
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

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "换目标")); err != nil {
		t.Fatalf("menu: %v", err)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m1b", "1")); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("choosing a squad dispatched")
	}
	if len(h.client.sent) < 2 || !strings.Contains(h.client.sent[1].CardJSON, "已选中 我的小队") {
		t.Fatalf("card = %+v", h.client.sent)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "整理本周纪要")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || !dispatcher.calls[0].Personal || dispatcher.calls[0].Description != "整理本周纪要" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	card := h.client.sent[len(h.client.sent)-1].CardJSON
	if !strings.Contains(card, "已交给 我的小队") {
		t.Fatalf("card = %s", card)
	}
}

func TestOrgBot_SentenceDoesNotSelectAProject(t *testing.T) {
	projectID := testUUID(0x31)
	dispatcher := &orgBotDispatcherFake{dispatch: service.OrgDispatchResult{
		Issue:   db.Issue{ID: testUUID(0x42), Number: 6, Title: "在aihub里面看下qwen4接了没"},
		Unit:    db.ListOrgUnitsRow{Name: "开发二组"},
		Project: &db.Workspace{Slug: "aihub", IssuePrefix: "AIHU", Name: "AIHUB"},
	}}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	router := payRoute()
	h.bot.cfg.Router = router
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: projectID, Name: "AIHUB", Path: "企业技术中心/研发部/开发二组",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{}

	sentence := "在aihub里面看下qwen4接了没"
	if _, err := h.bot.Handle(context.Background(), p2pText("m1", sentence)); err != nil {
		t.Fatalf("sentence: %v", err)
	}
	if len(dispatcher.calls) != 0 || router.calls != 0 {
		t.Fatalf("sentence selected a project: calls=%d routes=%d", len(dispatcher.calls), router.calls)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "AIHUB")); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ProjectID != projectID || dispatcher.calls[0].Description != sentence {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	if router.calls != 0 {
		t.Fatalf("route calls = %d, want 0", router.calls)
	}
}

func TestOrgBot_SwitchPhraseOpensThePicker(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("下一件")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "AIHUB", Path: "企业技术中心/研发部/开发二组",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectID:   testUUID(0x31),
		ProjectName: "AIHUB",
	}}

	for _, phrase := range []string{"我想换项目", "换部门", "我想换小队"} {
		if _, err := h.bot.Handle(context.Background(), p2pText(phrase, phrase)); err != nil {
			t.Fatalf("%s: %v", phrase, err)
		}
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("switch phrases created tasks: %+v", dispatcher.calls)
	}
	if len(h.client.sent) != 3 {
		t.Fatalf("cards = %d", len(h.client.sent))
	}
	for _, card := range h.client.sent {
		if !strings.Contains(card.CardJSON, "发给谁") {
			t.Fatalf("picker missing: %s", card.CardJSON)
		}
	}
}

func TestOrgBot_GroupSwitchesOnlyWhenTheKindIsNamed(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("下一件")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "AIHUB",
	}}}
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
	}}
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectID:   testUUID(0x31),
		ProjectName: "AIHUB",
	}}

	work := groupDispatchMessage("在aihub里面看下qwen4接了没")
	work.MessageID = "g1"
	if _, err := h.bot.Handle(context.Background(), work); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ProjectID != testUUID(0x31) || dispatcher.calls[0].Description != "在aihub里面看下qwen4接了没" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	if len(h.client.sent) == 0 || strings.Contains(h.client.sent[0].CardJSON, "切换") {
		t.Fatalf("group card offered a switch button: %+v", h.client.sent)
	}
	if len(h.client.menus) != 0 {
		t.Fatalf("group published an input menu: %+v", h.client.menus)
	}

	vague := groupDispatchMessage("切换")
	vague.MessageID = "g2"
	if _, err := h.bot.Handle(context.Background(), vague); err != nil {
		t.Fatalf("vague: %v", err)
	}
	if len(dispatcher.calls) != 2 || dispatcher.calls[1].ProjectID != testUUID(0x31) || dispatcher.calls[1].Description != "切换" {
		t.Fatalf("vague switch = %+v", dispatcher.calls)
	}

	for i, phrase := range []string{"换项目", "换部门", "我想换小队"} {
		msg := groupDispatchMessage(phrase)
		msg.MessageID = "g-switch-" + string(rune('a'+i))
		if _, err := h.bot.Handle(context.Background(), msg); err != nil {
			t.Fatalf("%s: %v", phrase, err)
		}
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("named switches created tasks: %+v", dispatcher.calls)
	}
	pickers := 0
	for _, card := range h.client.sent {
		if strings.Contains(card.CardJSON, "发给谁") {
			pickers++
		}
	}
	if pickers != 3 {
		t.Fatalf("pickers = %d", pickers)
	}

	click := groupDispatchMessage("target:menu")
	click.MessageID = "g-click"
	click.MessageType = orgBotCardActionType
	res, err := h.bot.Handle(context.Background(), click)
	if err != nil {
		t.Fatalf("button: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage || len(dispatcher.calls) != 2 {
		t.Fatalf("button switched: outcome %q calls %d", res.Outcome, len(dispatcher.calls))
	}
	if len(h.client.textSent) == 0 || !strings.Contains(h.client.textSent[len(h.client.textSent)-1].Text, "换项目") {
		t.Fatalf("button reply = %+v", h.client.textSent)
	}
}

func TestOrgBot_SwitchTargetFromTheDispatchedCard(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("下一件")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectID:   testUUID(0x31),
		ProjectName: "AIHUB",
		UnitPath:    "企业技术中心/研发部/开发二组",
	}}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "在aihub里面看下qwen4接了没")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ProjectID != testUUID(0x31) || dispatcher.calls[0].Description != "在aihub里面看下qwen4接了没" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	click := p2pText("c1", "target:menu")
	click.MessageType = orgBotCardActionType
	if _, err := h.bot.Handle(context.Background(), click); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "1")); err != nil {
		t.Fatalf("pick squad: %v", err)
	}
	if len(dispatcher.calls) != 1 {
		t.Fatalf("switching dispatched: %+v", dispatcher.calls)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m3", "整理本周纪要")); err != nil {
		t.Fatalf("next: %v", err)
	}
	if len(dispatcher.calls) != 2 || !dispatcher.calls[1].Personal || dispatcher.calls[1].Description != "整理本周纪要" {
		t.Fatalf("after switch = %+v", dispatcher.calls[1])
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

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "换目标")); err != nil {
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
	if res.Outcome != OutcomeDispatchAmbiguous || len(dispatcher.calls) != 0 {
		t.Fatalf("outcome = %q calls = %d", res.Outcome, len(dispatcher.calls))
	}
}

func TestOrgBot_StagedFileAttachesToCreatedTask(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:     orgBotTargetDepartment,
		UnitID:   testUUID(0x21),
		UnitPath: "财务/支付组",
	}}
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
	if len(h.client.sent) != 1 || !strings.Contains(h.client.sent[0].CardJSON, "已附上") {
		t.Fatalf("card = %+v", h.client.sent)
	}
}

func TestOrgBot_PostImageAttachesToTheTaskCreatedFromTheSameMessage(t *testing.T) {
	const requirement = "帮我看下这两个问题，解决掉"
	dispatch := createdPayDispatch(requirement)
	dispatch.Issue.WorkspaceID = testUUID(0x31)
	dispatch.Project = &db.Workspace{ID: testUUID(0x31), Name: "AIHUB", Slug: "aihub", IssuePrefix: "AIH"}
	dispatcher := &orgBotDispatcherFake{dispatch: dispatch}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectID:   testUUID(0x31),
		ProjectName: "AIHUB",
	}}
	h.client.resource = DownloadedResource{
		Data:        []byte("png-bytes"),
		ContentType: "image/png",
		Filename:    "shot.png",
	}

	post := p2pText("post-1", "[Image]\n"+requirement)
	post.MessageType = "post"
	post.Content = `{"content":[[{"tag":"img","image_key":"img_1"}],[{"tag":"text","text":"` + requirement + `"}]]}`

	if _, err := h.bot.Handle(context.Background(), post); err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatch calls = %d", len(dispatcher.calls))
	}
	if dispatcher.calls[0].Title != requirement || dispatcher.calls[0].Description != requirement {
		t.Fatalf("dispatch text = title %q description %q", dispatcher.calls[0].Title, dispatcher.calls[0].Description)
	}
	if len(h.attachments.attachments) != 1 {
		t.Fatalf("attachments = %d", len(h.attachments.attachments))
	}
	att := h.attachments.attachments[0]
	if att.IssueID != testUUID(0x42) || att.WorkspaceID != testUUID(0x31) || string(att.Data) != "png-bytes" {
		t.Fatalf("attachment = %+v", att)
	}
	if len(h.client.sent) != 1 || !strings.Contains(h.client.sent[0].CardJSON, requirement) ||
		strings.Contains(h.client.sent[0].CardJSON, "[Image]") {
		t.Fatalf("dispatch card = %+v", h.client.sent)
	}
}

func TestOrgBot_FileAfterDispatchWaitsForTheNextTask(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Context = &memOrgBotContext{ok: true, state: OrgBotChatContext{
		Kind:     orgBotTargetDepartment,
		UnitID:   testUUID(0x21),
		UnitPath: "财务/支付组",
	}}
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
	if len(h.attachments.attachments) != 0 {
		t.Fatalf("image attached to the previous task: %+v", h.attachments.attachments)
	}
	if len(h.client.textSent) != 1 || !strings.Contains(h.client.textSent[0].Text, "下一条要做的事") {
		t.Fatalf("reply = %+v", h.client.textSent)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "再查一下退款差异")); err != nil {
		t.Fatalf("next text: %v", err)
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("dispatch calls = %d, want 2", len(dispatcher.calls))
	}
	if len(h.attachments.attachments) != 1 ||
		h.attachments.attachments[0].IssueID != testUUID(0x42) {
		t.Fatalf("attachment = %+v", h.attachments.attachments)
	}
}

type orgBotDepartmentsFake struct {
	units []service.RouteCandidate
}

func (f orgBotDepartmentsFake) ListRequestableDepartments(context.Context, pgtype.UUID, pgtype.UUID) ([]service.RouteCandidate, error) {
	return f.units, nil
}

func TestOrgBotMenuKeyRoundTripAndCap(t *testing.T) {
	projectID := testUUID(0x31)
	key, ok := menuIDKey("p", projectID)
	if !ok || len(key) > 30 {
		t.Fatalf("key = %q ok %v", key, ok)
	}
	kind, raw, more, parsed := parseOrgBotMenuKey(key)
	if !parsed || more || kind != orgBotTargetProject || raw != util.UUIDToString(projectID) {
		t.Fatalf("parsed = %s %s more %v ok %v", kind, raw, more, parsed)
	}
	if kind, _, more, ok := parseOrgBotMenuKey("s"); !ok || more || kind != orgBotTargetPersonal {
		t.Fatalf("squad key = %s more %v ok %v", kind, more, ok)
	}

	projects := make([]service.RequestableProject, 0, 11)
	for i := byte(1); i <= 11; i++ {
		projects = append(projects, service.RequestableProject{ID: testUUID(i), Name: "项目" + string(rune('A'+i-1))})
	}
	items := orgBotMenuItems([]service.RouteCandidate{{
		UnitID: util.UUIDToString(testUUID(0x21)),
		Path:   "财务/支付组",
	}}, projects)
	if len(items) != 3 || items[0].Name != "我的小队" || items[0].EventKey != "s" || items[1].Name != "部门" || items[2].Name != "项目" {
		t.Fatalf("top = %+v", items)
	}
	children := items[2].Children
	if len(children) != orgBotMenuChildCap || children[len(children)-1].Name != "更多" || children[len(children)-1].EventKey != "mp" {
		t.Fatalf("project children = %+v", children)
	}
}

func TestOrgBot_OpeningTheChatPublishesThePersonalMenu(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "AIHUB",
	}}}
	entered := p2pText("m1", "")
	entered.MessageType = orgBotChatEnteredType
	res, err := h.bot.Handle(context.Background(), entered)
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage || len(h.client.textSent) != 0 || len(h.client.posts) != 1 || len(h.client.sent) != 0 {
		t.Fatalf("opening an empty chat: %+v text %d posts %+v", res, len(h.client.textSent), h.client.posts)
	}
	post := h.client.posts[0].Content
	for _, want := range []string{"告诉团队要做什么", "用一句话描述要做的事", `"italic"`, "可以这样说", "准备下周的客户上线材料", "- 排查昨天结算失败的原因"} {
		if !strings.Contains(post, want) {
			t.Fatalf("open guide missing %q: %s", want, post)
		}
	}

	again := p2pText("m2", "")
	again.MessageType = orgBotChatEnteredType
	again.LastVisibleMessageID = "om_kept"
	if _, err := h.bot.Handle(context.Background(), again); err != nil {
		t.Fatalf("reenter: %v", err)
	}
	if len(h.client.posts) != 1 || len(h.client.textSent) != 0 {
		t.Fatalf("a chat that still has history sent another guide: posts %d text %d", len(h.client.posts), len(h.client.textSent))
	}
	if len(h.client.menus) != 1 || !menuNames(h.client.menus[0].Items)["AIHUB"] || !menuNames(h.client.menus[0].Items)["我的小队"] {
		t.Fatalf("menus = %+v", h.client.menus)
	}
}

func TestOrgBot_MenuSelectsThenTheNextMessageIsTheRequirement(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("登录")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	project := service.RequestableProject{ID: testUUID(0x31), Name: "AIHUB", Path: "企业技术中心"}
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{project}}
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
	}}
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "换目标")); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	key, ok := menuIDKey("p", project.ID)
	if !ok {
		t.Fatal("project key")
	}
	click := p2pText("m2", key)
	click.ChatID = ""
	click.MessageType = orgBotMenuActionType
	if _, err := h.bot.Handle(context.Background(), click); err != nil {
		t.Fatalf("click: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("menu click dispatched: %+v", dispatcher.calls)
	}
	if len(h.client.sent) == 0 || !strings.Contains(h.client.sent[len(h.client.sent)-1].CardJSON, "已选中 项目 AIHUB") {
		t.Fatalf("selected card = %+v", h.client.sent)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m3", "看下 qwen4 接了没")); err != nil {
		t.Fatalf("requirement: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].ProjectID != project.ID || dispatcher.calls[0].Description != "看下 qwen4 接了没" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	if h.bot.cfg.Router.(*orgBotRouterFake).calls != 0 {
		t.Fatal("menu target was routed from the sentence")
	}
	if len(h.client.menus) == 0 || h.client.menus[0].OpenID != "ou_user" {
		t.Fatalf("menus = %+v", h.client.menus)
	}
	names := menuNames(h.client.menus[0].Items)
	for _, want := range []string{"我的小队", "部门", "项目", "财务/支付组", "AIHUB"} {
		if !names[want] {
			t.Fatalf("menu missing %s: %+v", want, h.client.menus[0].Items)
		}
	}
}

func TestOrgBot_MenuClickBeforeAnyChatPinsTheNextMessage(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("登录")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "AIHUB",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{}

	click := p2pText("m1", "s")
	click.ChatID = ""
	click.MessageType = orgBotMenuActionType
	if _, err := h.bot.Handle(context.Background(), click); err != nil {
		t.Fatalf("click: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("click dispatched")
	}
	if len(h.client.personal) != 1 || !strings.Contains(h.client.personal[0].CardJSON, "已选中 我的小队") {
		t.Fatalf("personal = %+v", h.client.personal)
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "整理本周纪要")); err != nil {
		t.Fatalf("requirement: %v", err)
	}
	if len(dispatcher.calls) != 1 || !dispatcher.calls[0].Personal || dispatcher.calls[0].Description != "整理本周纪要" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
}

func TestOrgBot_MenuSelectsADepartment(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
	}}
	h.bot.cfg.Context = &memOrgBotContext{}
	key, ok := menuIDKey("d", testUUID(0x21))
	if !ok {
		t.Fatal("key")
	}
	click := p2pText("m1", key)
	click.MessageType = orgBotMenuActionType
	if _, err := h.bot.Handle(context.Background(), click); err != nil {
		t.Fatalf("click: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatal("department click dispatched")
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "看一下对账")); err != nil {
		t.Fatalf("requirement: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].UnitID != testUUID(0x21) || dispatcher.calls[0].Description != "看一下对账" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
}

func TestOrgBot_MenuRejectsAProjectThePersonCannotHand(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("登录")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "AIHUB",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{}
	key, ok := menuIDKey("p", testUUID(0x99))
	if !ok {
		t.Fatal("key")
	}
	click := p2pText("m1", key)
	click.MessageType = orgBotMenuActionType
	res, err := h.bot.Handle(context.Background(), click)
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if res.Outcome != OutcomeDispatchNoAccess || len(dispatcher.calls) != 0 {
		t.Fatalf("outcome = %q calls = %d", res.Outcome, len(dispatcher.calls))
	}
}

func menuNames(items []BotMenuItem) map[string]bool {
	names := map[string]bool{}
	var walk func([]BotMenuItem)
	walk = func(items []BotMenuItem) {
		for _, item := range items {
			names[item.Name] = true
			walk(item.Children)
		}
	}
	walk(items)
	return names
}

func TestOrgBot_UnroutableOffersChoices(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = &orgBotRouterFake{err: service.ErrDispatchUnroutable}
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
	}}
	h.bot.cfg.Projects = orgBotProjectsFake{projects: []service.RequestableProject{{
		ID: testUUID(0x31), Name: "支付中台", Path: "财务/支付组",
	}}}
	h.bot.cfg.Context = &memOrgBotContext{}

	res, err := h.bot.Handle(context.Background(), p2pText("m1", "你会干啥"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchAmbiguous || len(dispatcher.calls) != 0 {
		t.Fatalf("outcome = %q calls = %d", res.Outcome, len(dispatcher.calls))
	}
	text := h.client.textSent[0].Text
	for _, want := range []string{"我的小队", "财务/支付组", "支付中台", "回复序号"} {
		if !strings.Contains(text, want) {
			t.Fatalf("reply missing %q: %q", want, text)
		}
	}
	if len(h.client.sent) != 1 || !strings.Contains(h.client.sent[0].CardJSON, "target:personal") {
		t.Fatalf("card = %+v", h.client.sent)
	}
}

func TestOrgBot_AskingForDepartmentsOffersChoices(t *testing.T) {
	dispatcher := &orgBotDispatcherFake{dispatch: createdPayDispatch("对账")}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), dispatcher)
	h.bot.cfg.Router = payRoute()
	h.bot.cfg.Departments = orgBotDepartmentsFake{units: []service.RouteCandidate{
		{UnitID: util.UUIDToString(testUUID(0x21)), Path: "财务/支付组"},
	}}
	h.bot.cfg.Context = &memOrgBotContext{}

	if _, err := h.bot.Handle(context.Background(), p2pText("m1", "你不能返回部门做选择吗")); err != nil {
		t.Fatalf("ask: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("asking for a choice dispatched")
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m2", "2")); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("picking dispatched the question")
	}
	if _, err := h.bot.Handle(context.Background(), p2pText("m3", "帮我看一下对账差异")); err != nil {
		t.Fatalf("work: %v", err)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0].UnitID != testUUID(0x21) || dispatcher.calls[0].Description != "帮我看一下对账差异" {
		t.Fatalf("dispatch = %+v", dispatcher.calls)
	}
	if h.bot.cfg.Router.(*orgBotRouterFake).calls != 0 {
		t.Fatalf("chosen department was routed again")
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
