package lark

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type orgBotIdentityFake struct {
	identity db.FeishuIdentity
	err      error
}

func (f orgBotIdentityFake) GetFeishuIdentityByOpenID(context.Context, string) (db.FeishuIdentity, error) {
	return f.identity, f.err
}

type orgBotProjectsFake struct {
	rows []db.ListDispatchProjectsForUserRow
	err  error
}

func (f orgBotProjectsFake) ListDispatchProjectsForUser(ctx context.Context, userID pgtype.UUID) ([]db.ListDispatchProjectsForUserRow, error) {
	return f.rows, f.err
}

type orgBotDedupFake struct {
	duplicate   bool
	claimErr    error
	processed   int
	released    int
	releaseErr  error
	lastMessage string
}

func (f *orgBotDedupFake) ClaimChannelInboundDedup(_ context.Context, arg db.ClaimChannelInboundDedupParams) (db.ChannelInboundMessageDedup, error) {
	f.lastMessage = arg.MessageID
	if f.claimErr != nil {
		return db.ChannelInboundMessageDedup{}, f.claimErr
	}
	if f.duplicate {
		return db.ChannelInboundMessageDedup{}, pgx.ErrNoRows
	}
	return db.ChannelInboundMessageDedup{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
		ClaimToken:     testUUID(0xCC),
	}, nil
}

func (f *orgBotDedupFake) MarkChannelInboundDedupProcessed(context.Context, db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	f.processed++
	return 1, nil
}

func (f *orgBotDedupFake) ReleaseChannelInboundDedup(context.Context, db.ReleaseChannelInboundDedupParams) (int64, error) {
	f.released++
	return 1, f.releaseErr
}

type orgBotDispatcherFake struct {
	matches     []service.UnitMatch
	resolveErr  error
	dispatch    service.OrgDispatchResult
	dispatchErr error
	calls       []service.OrgDispatchParams
}

func (f *orgBotDispatcherFake) Dispatch(_ context.Context, p service.OrgDispatchParams) (service.OrgDispatchResult, error) {
	f.calls = append(f.calls, p)
	return f.dispatch, f.dispatchErr
}

func (f *orgBotDispatcherFake) ResolveUnits(context.Context, pgtype.UUID, pgtype.UUID, string) ([]service.UnitMatch, error) {
	return f.matches, f.resolveErr
}

func testUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func testProject(id byte, name, slug, department string) db.ListDispatchProjectsForUserRow {
	return db.ListDispatchProjectsForUserRow{
		ID:             testUUID(id),
		Name:           name,
		Slug:           slug,
		IssuePrefix:    strings.ToUpper(name),
		DepartmentName: department,
	}
}

type orgBotHarness struct {
	bot         *OrgBot
	client      *fakeAPIClient
	identity    orgBotIdentityFake
	projects    orgBotProjectsFake
	dedup       *orgBotDedupFake
	dispatcher  *orgBotDispatcherFake
	chat        *orgBotChatFake
	attachments *orgBotAttachmentFake
}

func newOrgBotHarness(t *testing.T, identity orgBotIdentityFake, projects orgBotProjectsFake, dispatcher *orgBotDispatcherFake) *orgBotHarness {
	t.Helper()
	h := &orgBotHarness{
		client:      &fakeAPIClient{},
		identity:    identity,
		projects:    projects,
		dedup:       &orgBotDedupFake{},
		dispatcher:  dispatcher,
		chat:        &orgBotChatFake{},
		attachments: &orgBotAttachmentFake{},
	}
	bot, err := NewOrgBot(OrgBotConfig{
		InstallationID: testUUID(0xEE),
		BotOpenID:      "ou_bot",
		Credentials:    InstallationCredentials{AppID: "cli_login", AppSecret: "secret"},
		Connector:      NewNoopConnector(slog.New(slog.DiscardHandler)),
		Client:         h.client,
		Identity:       h.identity,
		Projects:       h.projects,
		Dedup:          h.dedup,
		Dispatcher:     h.dispatcher,
		Chat:           h.chat,
		Attachments:    h.attachments,
		AppURL:         "https://app.example.com",
		Logger:         slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewOrgBot: %v", err)
	}
	h.bot = bot
	return h
}

func groupDispatchMessage(body string) InboundMessage {
	return InboundMessage{
		ChatID:         "oc_chat",
		ChatType:       ChatTypeGroup,
		MessageID:      "om_1",
		SenderOpenID:   "ou_user",
		AddressedToBot: true,
		CommandBody:    body,
	}
}

func boundIdentity() orgBotIdentityFake {
	return orgBotIdentityFake{identity: db.FeishuIdentity{UserID: testUUID(0x11)}}
}

func TestOrgBot_DropsUnaddressedGroupMessage(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{}, &orgBotDispatcherFake{})
	msg := groupDispatchMessage("/dispatch 支付组")
	msg.AddressedToBot = false

	res, err := h.bot.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonNotAddressedInGroup {
		t.Fatalf("outcome = %q/%q, want dropped/not_addressed", res.Outcome, res.DropReason)
	}
	if len(h.client.textSent) != 0 {
		t.Fatalf("sent %d messages, want 0", len(h.client.textSent))
	}
}

func TestOrgBot_DuplicateClaimIsDropped(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: []db.ListDispatchProjectsForUserRow{testProject(1, "支付台", "pay", "研发部")}}, &orgBotDispatcherFake{})
	h.dedup.duplicate = true

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.DropReason != DropReasonDuplicate {
		t.Fatalf("drop reason = %q, want duplicate", res.DropReason)
	}
	if len(h.dispatcher.calls) != 0 {
		t.Fatalf("dispatched %d times, want 0", len(h.dispatcher.calls))
	}
}

func TestOrgBot_UnboundSenderGetsLoginPrompt(t *testing.T) {
	h := newOrgBotHarness(t,
		orgBotIdentityFake{err: pgx.ErrNoRows},
		orgBotProjectsFake{},
		&orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n目标: 对账\n验收: 一致"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeNeedsBinding {
		t.Fatalf("outcome = %q, want needs_binding", res.Outcome)
	}
	if len(h.client.textSent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(h.client.textSent))
	}
	if text := h.client.textSent[0].Text; !strings.Contains(text, "https://app.example.com/login") {
		t.Fatalf("login prompt missing link: %q", text)
	}
	if len(h.dispatcher.calls) != 0 {
		t.Fatalf("dispatched without an account")
	}
}

func TestOrgBot_IdentityLookupFailureRetriesInsteadOfPrompting(t *testing.T) {
	h := newOrgBotHarness(t,
		orgBotIdentityFake{err: errors.New("db down")},
		orgBotProjectsFake{},
		&orgBotDispatcherFake{})

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n目标: 对账\n验收: 一致")); err == nil {
		t.Fatal("Handle: want infrastructure error")
	}
	if len(h.client.textSent) != 0 {
		t.Fatalf("sent %d messages, want 0", len(h.client.textSent))
	}
	if h.dedup.released != 1 {
		t.Fatalf("released %d claims, want 1", h.dedup.released)
	}
}

func TestOrgBot_MultipleProjectsRequireSelector(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{
		testProject(1, "支付台", "pay", "研发部"),
		testProject(2, "风控台", "risk", "研发部"),
	}
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: projects}, &orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n目标: 对账\n验收: 一致"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchAmbiguous {
		t.Fatalf("outcome = %q, want dispatch_ambiguous", res.Outcome)
	}
	text := h.client.textSent[0].Text
	for _, want := range []string{"支付台", "风控台", "项目"} {
		if !strings.Contains(text, want) {
			t.Fatalf("choose-project reply missing %q: %q", want, text)
		}
	}
}

func TestOrgBot_ProjectSelectorAndDispatch(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{
		testProject(1, "支付台", "pay", "研发部"),
		testProject(2, "风控台", "risk", "研发部"),
	}
	d := &orgBotDispatcherFake{
		matches: []service.UnitMatch{{Unit: db.ListOrgUnitsRow{ID: testUUID(0x31), Name: "支付组"}, Path: "研发部 / 支付组"}},
		dispatch: service.OrgDispatchResult{
			Issue: db.Issue{ID: testUUID(0x41), Number: 7, Title: "对账"},
			Unit:  db.ListOrgUnitsRow{Name: "支付组"},
		},
	}
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: projects}, d)

	msg := groupDispatchMessage("/dispatch 支付组\n核对账单\n项目: 2\n目标: 对账\n验收: 一致")
	res, err := h.bot.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchCreated {
		t.Fatalf("outcome = %q, want dispatch_created", res.Outcome)
	}
	if len(d.calls) != 1 {
		t.Fatalf("dispatch calls = %d, want 1", len(d.calls))
	}
	call := d.calls[0]
	if call.WorkspaceID != projects[1].ID {
		t.Fatalf("workspace = %v, want %v", call.WorkspaceID, projects[1].ID)
	}
	if call.Origin.InstallationID != testUUID(0xEE) || call.Origin.ChatID != "oc_chat" {
		t.Fatalf("origin not recorded: %+v", call.Origin)
	}
	if call.Origin.ChannelType != service.DispatchChannelFeishu {
		t.Fatalf("channel = %q", call.Origin.ChannelType)
	}
	if !strings.Contains(call.Description, "## 目标") || !strings.Contains(call.Description, "## 验收") {
		t.Fatalf("description not sectioned: %q", call.Description)
	}
	text := h.client.textSent[0].Text
	if !strings.Contains(text, "风控台") || !strings.Contains(text, "研发部 / 支付组") {
		t.Fatalf("confirmation missing project or unit: %q", text)
	}
	if !strings.Contains(text, "/risk/") {
		t.Fatalf("confirmation missing project deep link: %q", text)
	}
	if h.dedup.processed != 1 {
		t.Fatalf("processed = %d, want 1", h.dedup.processed)
	}
	if h.dedup.released != 0 {
		t.Fatalf("released = %d, want 0", h.dedup.released)
	}
}

func TestOrgBot_UnknownProjectSelector(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{testProject(1, "支付台", "pay", "研发部")}
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: projects}, &orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n项目: 物流台\n目标: 对账\n验收: 一致"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchNoAccess {
		t.Fatalf("outcome = %q, want dispatch_no_access", res.Outcome)
	}
	if !strings.Contains(h.client.textSent[0].Text, "没有找到项目") {
		t.Fatalf("unexpected reply: %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_ProjectsCommandListsSenderProjects(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{
		testProject(1, "支付台", "pay", "研发部"),
		testProject(2, "风控台", "risk", "研发部"),
	}
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: projects}, &orgBotDispatcherFake{})

	if _, err := h.bot.Handle(context.Background(), groupDispatchMessage("/projects")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	text := h.client.textSent[0].Text
	if !strings.Contains(text, "1. 支付台") || !strings.Contains(text, "2. 风控台") {
		t.Fatalf("project list: %q", text)
	}
}

func TestOrgBot_UnboundDispatchOutcome(t *testing.T) {
	d := &orgBotDispatcherFake{
		matches:     []service.UnitMatch{{Unit: db.ListOrgUnitsRow{ID: testUUID(0x31)}, Path: "研发部 / 支付组"}},
		dispatchErr: service.ErrDispatchRequesterUnbound,
	}
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{rows: []db.ListDispatchProjectsForUserRow{testProject(1, "支付台", "pay", "研发部")}}, d)

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n目标: 对账\n验收: 一致"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchUnbound {
		t.Fatalf("outcome = %q, want dispatch_unbound", res.Outcome)
	}
	if h.client.textSent[0].Text != dispatchUnboundCopy {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestOrgBot_UsageCopyOnMalformedCommand(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), orgBotProjectsFake{}, &orgBotDispatcherFake{})

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n只有标题"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage {
		t.Fatalf("outcome = %q, want dispatch_usage", res.Outcome)
	}
	if h.client.textSent[0].Text != orgBotUsageCopy {
		t.Fatalf("reply = %q", h.client.textSent[0].Text)
	}
}

func TestSelectOrgProject(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{
		testProject(1, "支付台", "pay", "研发部"),
		testProject(2, "支付网关", "pay-gw", "研发部"),
		testProject(3, "风控台", "risk", "风控部"),
	}
	cases := []struct {
		name     string
		selector string
		wantID   byte
		found    bool
	}{
		{name: "empty ambiguous", selector: "", found: false},
		{name: "number", selector: "2", wantID: 2, found: true},
		{name: "hash number", selector: "#3", wantID: 3, found: true},
		{name: "out of range", selector: "9", found: false},
		{name: "exact name", selector: "支付台", wantID: 1, found: true},
		{name: "exact slug", selector: "risk", wantID: 3, found: true},
		{name: "substring", selector: "风控", wantID: 3, found: true},
		{name: "ambiguous substring", selector: "支付", found: false},
		{name: "miss", selector: "物流", found: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, found := selectOrgProject(projects, tc.selector)
			if found != tc.found {
				t.Fatalf("found = %v, want %v", found, tc.found)
			}
			if found && got.ID.Bytes[0] != tc.wantID {
				t.Fatalf("selected %v, want %v", got.ID, tc.wantID)
			}
		})
	}
}

func TestSelectOrgProject_SingleProjectNeedsNoSelector(t *testing.T) {
	projects := []db.ListDispatchProjectsForUserRow{testProject(1, "支付台", "pay", "研发部")}
	got, _, found := selectOrgProject(projects, "")
	if !found || got.Name != "支付台" {
		t.Fatalf("selected %+v found=%v", got, found)
	}
}

var _ engine.OrgDispatcher = (*orgBotDispatcherFake)(nil)
