package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

const (
	testClientRequestID  = "11111111-1111-1111-1111-111111111111"
	testClientRequestID2 = "22222222-2222-2222-2222-222222222222"
)

func postDispatchWork(t *testing.T, userID string, body map[string]any) (int, dispatchWorkResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.DispatchWork(rec, newRequestAs(userID, "POST", "/api/org/dispatch", body))
	var res dispatchWorkResponse
	if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode dispatch work response: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res, rec.Body.String()
}

func listDispatches(t *testing.T, userID, query string) (int, []dispatchListItem, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ListMyDispatches(rec, newRequestAs(userID, "GET", "/api/org/dispatches"+query, nil))
	var res struct {
		Dispatches []dispatchListItem `json:"dispatches"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode dispatch list: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res.Dispatches, rec.Body.String()
}

func countDispatchOrigins(t *testing.T) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM dispatch_origin WHERE workspace_id = $1
	`, testWorkspaceID).Scan(&n); err != nil {
		t.Fatalf("count dispatch origins: %v", err)
	}
	return n
}

func TestDispatchWork_RoutesToOnlyDepartmentAndDerivesTitle(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-route-admin@example.com")
	leader := insertRunnableDutyAgent(t, "workbench-route-leader", admin)
	unitID := createOrgUnitAs(t, admin, "自动路由部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 准备 onboarding 材料\n下周客户要来",
		"client_request_id": testClientRequestID,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.UnitID != unitID || res.Dispatch.UnitPath != "自动路由部" {
		t.Fatalf("dispatch = %+v, want unit %s", res.Dispatch, unitID)
	}
	if got, want := res.Issue.Title, "dispatch-test 准备 onboarding 材料"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	// The whole request is the description, not just the title line.
	if res.Issue.Description == nil || *res.Issue.Description != "dispatch-test 准备 onboarding 材料\n下周客户要来" {
		t.Fatalf("description = %v", res.Issue.Description)
	}

	var storedClientID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT client_request_id FROM dispatch_origin WHERE issue_id = $1
	`, res.Issue.ID).Scan(&storedClientID); err != nil {
		t.Fatalf("load client_request_id: %v", err)
	}
	if storedClientID != testClientRequestID {
		t.Fatalf("client_request_id = %q, want %q", storedClientID, testClientRequestID)
	}
}

func TestDispatchWork_BindsUploadedAttachmentsToTheRootIssue(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-attach-admin@example.com")
	leader := insertRunnableDutyAgent(t, "workbench-attach-leader", admin)
	unitID := createOrgUnitAs(t, admin, "附件部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'brief.pdf', 'https://files.example/brief.pdf', 'application/pdf', 2048)
		RETURNING id::text
	`, testWorkspaceID, admin).Scan(&attachmentID); err != nil {
		t.Fatalf("seed unbound attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})

	status, res, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 按附件准备材料",
		"client_request_id": testClientRequestID,
		"attachment_ids":    []string{attachmentID},
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}

	var boundIssueID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT issue_id::text FROM attachment WHERE id = $1
	`, attachmentID).Scan(&boundIssueID); err != nil {
		t.Fatalf("load attachment binding: %v", err)
	}
	if boundIssueID != res.Issue.ID {
		t.Fatalf("attachment issue = %s, want root issue %s", boundIssueID, res.Issue.ID)
	}
}

func TestDispatchWork_RejectsMalformedAttachmentIDs(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-attach-bad@example.com")

	status, _, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 坏附件",
		"client_request_id": testClientRequestID,
		"attachment_ids":    []string{"not-a-uuid"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, body)
	}
	if n := countDispatchOrigins(t); n != 0 {
		t.Fatalf("dispatch origins = %d, want 0", n)
	}
}

func TestDispatchWork_RetryReturnsTheSameIssue(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-idempotent-admin@example.com")
	leader := insertRunnableDutyAgent(t, "workbench-idempotent-leader", admin)
	unitID := createOrgUnitAs(t, admin, "幂等部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	request := map[string]any{
		"text":              "dispatch-test 幂等请求",
		"client_request_id": testClientRequestID,
	}
	status, first, body := postDispatchWork(t, admin, request)
	if status != http.StatusCreated {
		t.Fatalf("first status = %d, want 201: %s", status, body)
	}

	status, second, body := postDispatchWork(t, admin, request)
	if status != http.StatusOK {
		t.Fatalf("retry status = %d, want 200: %s", status, body)
	}
	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("retry issue = %s, want %s", second.Issue.ID, first.Issue.ID)
	}
	if n := countDispatchOrigins(t); n != 1 {
		t.Fatalf("dispatch origins = %d, want 1", n)
	}
}

func TestDispatchWork_OffersCandidatesWhenTextMatchesTwoDepartments(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-ambiguous-admin@example.com")
	payAgent := insertRunnableDutyAgent(t, "workbench-pay-agent", admin)
	settleAgent := insertRunnableDutyAgent(t, "workbench-settle-agent", admin)
	payUnit := createOrgUnitAs(t, admin, "支付组", "")
	settleUnit := createOrgUnitAs(t, admin, "结算组", "")
	placeOrgAgent(t, admin, payAgent, payUnit)
	placeOrgAgent(t, admin, settleAgent, settleUnit)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	rec := httptest.NewRecorder()
	testHandler.DispatchWork(rec, newRequestAs(admin, "POST", "/api/org/dispatch", map[string]any{
		"text":              "dispatch-test 支付组和结算组一起看下",
		"client_request_id": testClientRequestID,
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code       string                   `json:"code"`
		Candidates []service.RouteCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode candidates: %v %s", err, rec.Body.String())
	}
	if body.Code != "org_dispatch_ambiguous" || len(body.Candidates) != 2 {
		t.Fatalf("response = %+v, want two candidates", body)
	}
	if n := countDispatchOrigins(t); n != 0 {
		t.Fatalf("ambiguous request created %d origins, want 0", n)
	}
}

func TestDispatchWork_UsesModelChoiceWhenDeterministicMatchFails(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-llm-admin@example.com")
	payAgent := insertRunnableDutyAgent(t, "workbench-llm-pay-agent", admin)
	settleAgent := insertRunnableDutyAgent(t, "workbench-llm-settle-agent", admin)
	payUnit := createOrgUnitAs(t, admin, "支付组", "")
	settleUnit := createOrgUnitAs(t, admin, "结算组", "")
	placeOrgAgent(t, admin, payAgent, payUnit)
	placeOrgAgent(t, admin, settleAgent, settleUnit)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	previous := testHandler.OrgDispatchService.LLM
	testHandler.OrgDispatchService.LLM = fakeTextGenerator(func(string, string) (string, error) {
		return `{"unit_id":"` + settleUnit + `"}`, nil
	})
	t.Cleanup(func() { testHandler.OrgDispatchService.LLM = previous })

	status, res, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 把昨天的对账单核一下",
		"client_request_id": testClientRequestID,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.UnitID != settleUnit {
		t.Fatalf("unit = %s, want model choice %s", res.Dispatch.UnitID, settleUnit)
	}
}

func TestDispatchWork_RejectsInvalidModelChoice(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-llm-invalid-admin@example.com")
	payAgent := insertRunnableDutyAgent(t, "workbench-llm-invalid-pay", admin)
	settleAgent := insertRunnableDutyAgent(t, "workbench-llm-invalid-settle", admin)
	payUnit := createOrgUnitAs(t, admin, "支付组", "")
	settleUnit := createOrgUnitAs(t, admin, "结算组", "")
	placeOrgAgent(t, admin, payAgent, payUnit)
	placeOrgAgent(t, admin, settleAgent, settleUnit)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	previous := testHandler.OrgDispatchService.LLM
	testHandler.OrgDispatchService.LLM = fakeTextGenerator(func(string, string) (string, error) {
		return `{"unit_id":"not-a-listed-unit"}`, nil
	})
	t.Cleanup(func() { testHandler.OrgDispatchService.LLM = previous })

	// An id outside the candidate list is ignored, so the request falls through
	// to the deterministic matchers and ends ambiguous instead of misrouting.
	status, _, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 把昨天的对账单核一下",
		"client_request_id": testClientRequestID,
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", status, body)
	}
}

func TestDispatchWork_ValidatesClientRequestID(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-validate-admin@example.com")

	for name, body := range map[string]map[string]any{
		"missing":    {"text": "dispatch-test 没有请求 id"},
		"not a uuid": {"text": "dispatch-test 请求 id 非法", "client_request_id": "retry-1"},
	} {
		t.Run(name, func(t *testing.T) {
			status, _, raw := postDispatchWork(t, admin, body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", status, raw)
			}
		})
	}
}

func TestListMyDispatches_ReturnsOnlyTheRequesterWithPaths(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-list-admin@example.com")
	otherAdmin := createOrgTestAdmin(t, "workbench-list-other@example.com")
	leader := insertRunnableDutyAgent(t, "workbench-list-leader", admin)
	unitID := createOrgUnitAs(t, admin, "历史部", "")
	placeOrgAgent(t, admin, leader, unitID)
	otherAgent := insertRunnableDutyAgent(t, "workbench-list-other-agent", otherAdmin)
	otherUnit := createOrgUnitAs(t, admin, "历史二部", "")
	placeOrgAgent(t, admin, otherAgent, otherUnit)
	setOrgUnitLead(t, otherUnit, otherAdmin)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	mine := make([]dispatchWorkResponse, 0, 2)
	for i, clientID := range []string{testClientRequestID, testClientRequestID2} {
		status, res, body := postDispatchWork(t, admin, map[string]any{
			"text":              "dispatch-test 历史请求 " + string(rune('A'+i)),
			"unit_id":           unitID,
			"client_request_id": clientID,
		})
		if status != http.StatusCreated {
			t.Fatalf("dispatch %d status = %d: %s", i, status, body)
		}
		mine = append(mine, res)
	}
	if status, _, body := postDispatchWork(t, otherAdmin, map[string]any{
		"text":              "dispatch-test 别人的请求",
		"unit_id":           otherUnit,
		"client_request_id": testClientRequestID,
	}); status != http.StatusCreated {
		t.Fatalf("other admin dispatch status = %d: %s", status, body)
	}

	// Give one dispatch a completion receipt.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE dispatch_origin SET notified_at = now() WHERE issue_id = $1
	`, mine[0].Issue.ID); err != nil {
		t.Fatalf("mark dispatch complete: %v", err)
	}

	status, items, body := listDispatches(t, admin, "?limit=30")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(items) != 2 {
		t.Fatalf("dispatches = %d, want only the requester's 2: %s", len(items), body)
	}
	byIssue := map[string]dispatchListItem{}
	for _, item := range items {
		byIssue[item.Issue.ID] = item
		if item.UnitPath != "历史部" {
			t.Fatalf("unit path = %q, want 历史部", item.UnitPath)
		}
	}
	completed, ok := byIssue[mine[0].Issue.ID]
	if !ok || !completed.Completed || completed.NotifiedAt == nil {
		t.Fatalf("completed item = %+v, want completed with notified_at", completed)
	}
	pending, ok := byIssue[mine[1].Issue.ID]
	if !ok || pending.Completed || pending.NotifiedAt != nil {
		t.Fatalf("pending item = %+v, want no receipt", pending)
	}

	status, items, body = listDispatches(t, admin, "?limit=1")
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("limit=1 status/items = %d/%d: %s", status, len(items), body)
	}
	if status, _, _ := listDispatches(t, admin, "?limit=0"); status != http.StatusBadRequest {
		t.Fatalf("limit=0 status = %d, want 400", status)
	}
}

func TestDispatchWork_UnboundMemberCannotDispatch(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-unbound-admin@example.com")
	leader := insertRunnableDutyAgent(t, "workbench-unbound-leader", admin)
	unitID := createOrgUnitAs(t, admin, "未绑定部", "")
	placeOrgAgent(t, admin, leader, unitID)
	member := createPermissionTestMember(t, "workbench-unbound-member@example.com")
	// The member leads the department, so visibility passes and the missing
	// agent binding is what stops the dispatch.
	setOrgUnitLead(t, unitID, member)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, _, body := postDispatchWork(t, member, map[string]any{
		"text":              "dispatch-test 未绑定派活",
		"unit_id":           unitID,
		"client_request_id": testClientRequestID,
	})
	if status != http.StatusForbidden || !containsCode(body, "agent_binding_required") {
		t.Fatalf("status = %d, body = %s, want 403 agent_binding_required", status, body)
	}
	if n := countDispatchOrigins(t); n != 0 {
		t.Fatalf("dispatch origins = %d, want 0", n)
	}
}

func TestDispatchWork_ExplicitUnitOutsideScopeIsNotFound(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "workbench-scope-admin@example.com")
	member := createPermissionTestMember(t, "workbench-scope-member@example.com")
	adminAgent := insertRunnableDutyAgent(t, "workbench-scope-admin-agent", admin)
	ownUnit := createOrgUnitAs(t, admin, "成员部", "")
	hiddenUnit := createOrgUnitAs(t, admin, "隐藏部", "")
	placeOrgMember(t, member, ownUnit)
	placeOrgAgent(t, admin, adminAgent, hiddenUnit)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, _, body := postDispatchWork(t, member, map[string]any{
		"text":              "dispatch-test 越权派活",
		"unit_id":           hiddenUnit,
		"client_request_id": testClientRequestID,
	})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", status, body)
	}
}

// fakeTextGenerator stands in for the routing model without touching a
// provider. The workbench tests use it to pin both a valid and an invalid
// model answer.
type fakeTextGenerator func(systemPrompt, userPrompt string) (string, error)

func (f fakeTextGenerator) GenerateText(_ context.Context, _, systemPrompt, userPrompt string) (string, error) {
	return f(systemPrompt, userPrompt)
}
