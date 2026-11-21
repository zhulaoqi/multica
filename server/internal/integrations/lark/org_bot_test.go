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

type orgBotOrganizationFake struct {
	org db.Workspace
	err error
}

func (f orgBotOrganizationFake) GetOrganizationWorkspace(context.Context) (db.Workspace, error) {
	return f.org, f.err
}

func testOrganization() orgBotOrganizationFake {
	return orgBotOrganizationFake{org: db.Workspace{ID: testUUID(0x01), Name: "公司", Slug: "org", IssuePrefix: "ORG"}}
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

type orgBotHarness struct {
	bot         *OrgBot
	client      *fakeAPIClient
	identity    orgBotIdentityFake
	org         orgBotOrganizationFake
	dedup       *orgBotDedupFake
	dispatcher  *orgBotDispatcherFake
	chat        *orgBotChatFake
	attachments *orgBotAttachmentFake
}

func newOrgBotHarness(t *testing.T, identity orgBotIdentityFake, org orgBotOrganizationFake, dispatcher *orgBotDispatcherFake) *orgBotHarness {
	t.Helper()
	h := &orgBotHarness{
		client:      &fakeAPIClient{},
		identity:    identity,
		org:         org,
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
		Organization:   h.org,
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
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
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
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
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

func TestOrgBot_EmptyP2PMessageGetsReply(t *testing.T) {
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})
	msg := InboundMessage{
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    "om_dm_sticker",
		SenderOpenID: "ou_user",
		MessageType:  "sticker",
	}

	res, err := h.bot.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchUsage {
		t.Fatalf("outcome = %q, want dispatch_usage", res.Outcome)
	}
	if len(h.client.textSent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(h.client.textSent))
	}
	if text := h.client.textSent[0].Text; !strings.Contains(text, "/dispatch") {
		t.Fatalf("sticker reply missing dispatch hint: %q", text)
	}
}

func TestOrgBot_UnboundSenderGetsLoginPrompt(t *testing.T) {
	h := newOrgBotHarness(t,
		orgBotIdentityFake{err: pgx.ErrNoRows},
		testOrganization(),
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
		testOrganization(),
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

func TestOrgBot_DispatchesIntoOrganization(t *testing.T) {
	d := &orgBotDispatcherFake{
		matches: []service.UnitMatch{{Unit: db.ListOrgUnitsRow{ID: testUUID(0x31), Name: "支付组"}, Path: "研发部 / 支付组"}},
		dispatch: service.OrgDispatchResult{
			Issue: db.Issue{ID: testUUID(0x41), Number: 7, Title: "对账"},
			Unit:  db.ListOrgUnitsRow{Name: "支付组"},
		},
	}
	org := testOrganization()
	h := newOrgBotHarness(t, boundIdentity(), org, d)

	msg := groupDispatchMessage("/dispatch 支付组\n核对账单\n目标: 对账\n验收: 一致")
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
	if call.WorkspaceID != org.org.ID {
		t.Fatalf("workspace = %v, want the organization %v", call.WorkspaceID, org.org.ID)
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
	if !strings.Contains(text, "研发部 / 支付组") || !strings.Contains(text, "ORG-7") {
		t.Fatalf("confirmation missing unit or identifier: %q", text)
	}
	if !strings.Contains(text, "/org/") {
		t.Fatalf("confirmation missing organization deep link: %q", text)
	}
	if res.IssueWorkspaceSlug != "org" {
		t.Fatalf("issue workspace slug = %q, want org", res.IssueWorkspaceSlug)
	}
	if h.dedup.processed != 1 || h.dedup.released != 0 {
		t.Fatalf("processed = %d released = %d, want 1/0", h.dedup.processed, h.dedup.released)
	}
}

func TestOrgBot_NoOrganizationRefusesDispatch(t *testing.T) {
	d := &orgBotDispatcherFake{}
	h := newOrgBotHarness(t, boundIdentity(), orgBotOrganizationFake{err: pgx.ErrNoRows}, d)

	res, err := h.bot.Handle(context.Background(), groupDispatchMessage("/dispatch 支付组\n标题\n目标: 对账\n验收: 一致"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Outcome != OutcomeDispatchNoAccess || h.client.textSent[0].Text != orgBotNoOrganizationCopy {
		t.Fatalf("outcome = %q reply = %q", res.Outcome, h.client.textSent[0].Text)
	}
	if len(d.calls) != 0 {
		t.Fatalf("dispatched %d times without an organization", len(d.calls))
	}
}

func TestOrgBot_UnboundDispatchOutcome(t *testing.T) {
	d := &orgBotDispatcherFake{
		matches:     []service.UnitMatch{{Unit: db.ListOrgUnitsRow{ID: testUUID(0x31)}, Path: "研发部 / 支付组"}},
		dispatchErr: service.ErrDispatchRequesterUnbound,
	}
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), d)

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
	h := newOrgBotHarness(t, boundIdentity(), testOrganization(), &orgBotDispatcherFake{})

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

var _ engine.OrgDispatcher = (*orgBotDispatcherFake)(nil)
