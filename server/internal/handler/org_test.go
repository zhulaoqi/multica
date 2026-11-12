package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func createOrgTestAdmin(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create admin user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add admin %s: %v", email, err)
	}
	return userID
}

func cleanupOrgUnits(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func TestImportOrgUnits_MemberForbidden(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	member := createPermissionTestMember(t, "org-forbidden@example.com")

	rec := httptest.NewRecorder()
	testHandler.ImportOrgUnits(rec, newRequestAs(member, "POST", "/api/org/units/import", map[string]any{
		"rows": []map[string]any{{"path": "平台部"}},
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestImportOrgUnits_CreatesTreeAssignsLeadAndAgents(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-admin@example.com")
	lead := createPermissionTestMember(t, "org-lead@example.com")
	payAgent := insertIntakeDutyAgent(t, "org-pay-agent", admin)
	settleAgent := insertIntakeDutyAgent(t, "org-settle-agent", admin)

	body := map[string]any{"rows": []map[string]any{
		{"path": "平台部/支付组", "lead_email": "ORG-Lead@example.com", "agents": []string{"org-pay-agent"}},
		{"path": "平台部/结算组", "agents": []string{"org-settle-agent", "不存在的Agent"}},
		{"path": "数据部", "lead_email": "nobody@example.com"},
	}}
	rec := httptest.NewRecorder()
	testHandler.ImportOrgUnits(rec, newRequestAs(admin, "POST", "/api/org/units/import", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res orgImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.CreatedUnits != 4 {
		t.Fatalf("created_units = %d, want 4 (%+v)", res.CreatedUnits, res)
	}
	if res.AgentsAssigned != 2 {
		t.Fatalf("agents_assigned = %d, want 2 (%+v)", res.AgentsAssigned, res)
	}
	if len(res.UnknownAgents) != 1 || res.UnknownAgents[0] != "不存在的Agent" {
		t.Fatalf("unknown_agents = %v", res.UnknownAgents)
	}
	if len(res.UnknownLeads) != 1 || res.UnknownLeads[0] != "nobody@example.com" {
		t.Fatalf("unknown_leads = %v", res.UnknownLeads)
	}

	list := httptest.NewRecorder()
	testHandler.ListOrgUnits(list, newRequestAs(admin, "GET", "/api/org/units", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
	}
	var tree orgTreeResponse
	if err := json.Unmarshal(list.Body.Bytes(), &tree); err != nil {
		t.Fatalf("decode tree: %v", err)
	}
	byName := map[string]orgUnitResponse{}
	for _, unit := range tree.Units {
		byName[unit.Name] = unit
	}
	platform, ok := byName["平台部"]
	if !ok {
		t.Fatalf("平台部 missing from %v", byName)
	}
	if platform.ParentID != "" {
		t.Fatalf("平台部 parent = %q, want root", platform.ParentID)
	}
	pay, ok := byName["支付组"]
	if !ok {
		t.Fatalf("支付组 missing from %v", byName)
	}
	if pay.ParentID != platform.ID {
		t.Fatalf("支付组 parent = %q, want %q", pay.ParentID, platform.ID)
	}
	if pay.LeadUserID != lead {
		t.Fatalf("支付组 lead = %q, want %q", pay.LeadUserID, lead)
	}
	if got := byName["结算组"].ParentID; got != platform.ID {
		t.Fatalf("结算组 parent = %q, want %q", got, platform.ID)
	}
	if got := byName["数据部"].LeadUserID; got != "" {
		t.Fatalf("数据部 lead = %q, want empty for unknown lead", got)
	}
	agentUnits := map[string]string{}
	agentIDs := map[string]string{}
	for _, agent := range tree.Agents {
		agentUnits[agent.Name] = agent.UnitID
		agentIDs[agent.Name] = agent.ID
	}
	if agentIDs["org-pay-agent"] != payAgent {
		t.Fatalf("pay agent id = %q, want %q", agentIDs["org-pay-agent"], payAgent)
	}
	if agentUnits["org-pay-agent"] != pay.ID {
		t.Fatalf("pay agent unit = %q, want %q", agentUnits["org-pay-agent"], pay.ID)
	}
	if agentUnits["org-settle-agent"] != byName["结算组"].ID {
		t.Fatalf("settle agent unit = %q, want %q", agentUnits["org-settle-agent"], byName["结算组"].ID)
	}

	roster := httptest.NewRecorder()
	testHandler.ListIntakeAgents(roster, newRequestAs(admin, "GET", "/api/intake/agents", nil))
	if roster.Code != http.StatusOK {
		t.Fatalf("roster status = %d: %s", roster.Code, roster.Body.String())
	}
	var listed []intakeAgentResponse
	if err := json.Unmarshal(roster.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	rosterPaths := map[string]string{}
	for _, agent := range listed {
		rosterPaths[agent.Name] = agent.OrgPath
	}
	if rosterPaths["org-pay-agent"] != "平台部/支付组" {
		t.Fatalf("pay agent org_path = %q, want 平台部/支付组", rosterPaths["org-pay-agent"])
	}

	// Re-running the same import must not duplicate units, and moving an agent
	// must land it in the new unit.
	body["rows"] = []map[string]any{
		{"path": "平台部/支付组", "lead_email": "org-lead@example.com"},
		{"path": "平台部/风控组", "agents": []string{"org-settle-agent"}},
	}
	again := httptest.NewRecorder()
	testHandler.ImportOrgUnits(again, newRequestAs(admin, "POST", "/api/org/units/import", body))
	if again.Code != http.StatusOK {
		t.Fatalf("second import status = %d: %s", again.Code, again.Body.String())
	}
	if err := json.Unmarshal(again.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if res.CreatedUnits != 1 || res.ExistingUnits != 2 {
		t.Fatalf("second import created=%d existing=%d, want 1/2 (%+v)", res.CreatedUnits, res.ExistingUnits, res)
	}
	var unitCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM org_unit WHERE workspace_id = $1
	`, testWorkspaceID).Scan(&unitCount); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if unitCount != 5 {
		t.Fatalf("org_unit rows = %d, want 5", unitCount)
	}
	var settleUnit, riskUnit string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM org_unit WHERE workspace_id = $1 AND name = '风控组'
	`, testWorkspaceID).Scan(&riskUnit); err != nil {
		t.Fatalf("load 风控组: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT org_unit_id FROM agent WHERE id = $1
	`, settleAgent).Scan(&settleUnit); err != nil {
		t.Fatalf("load agent unit: %v", err)
	}
	if settleUnit != riskUnit {
		t.Fatalf("settle agent unit = %q, want 风控组 %q", settleUnit, riskUnit)
	}
}

func TestImportOrgUnits_RejectsBadRows(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-admin-bad@example.com")

	for name, body := range map[string]map[string]any{
		"empty":         {"rows": []map[string]any{}},
		"blank segment": {"rows": []map[string]any{{"path": "平台部//支付组"}}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testHandler.ImportOrgUnits(rec, newRequestAs(admin, "POST", "/api/org/units/import", body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
	var unitCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM org_unit WHERE workspace_id = $1
	`, testWorkspaceID).Scan(&unitCount); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if unitCount != 0 {
		t.Fatalf("org_unit rows = %d, want 0 after rejected imports", unitCount)
	}
}

func orgRequestWithAgent(userID, method, path, agentID string, body any) *http.Request {
	req := newRequestAs(userID, method, path, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentId", agentID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestSetOrgAgentUnit_MemberForbiddenAdminMovesAndClears(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-move-admin@example.com")
	member := createPermissionTestMember(t, "org-move-member@example.com")
	agentID := insertIntakeDutyAgent(t, "org-move-agent", admin)

	rec := httptest.NewRecorder()
	testHandler.ImportOrgUnits(rec, newRequestAs(admin, "POST", "/api/org/units/import", map[string]any{
		"rows": []map[string]any{{"path": "平台部/支付组"}},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", rec.Code, rec.Body.String())
	}
	var unitID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM org_unit WHERE workspace_id = $1 AND name = '支付组'
	`, testWorkspaceID).Scan(&unitID); err != nil {
		t.Fatalf("load 支付组: %v", err)
	}

	memberRec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(memberRec, orgRequestWithAgent(member, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{"unit_id": unitID}))
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403: %s", memberRec.Code, memberRec.Body.String())
	}

	moveRec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(moveRec, orgRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{"unit_id": unitID}))
	if moveRec.Code != http.StatusNoContent {
		t.Fatalf("move status = %d, want 204: %s", moveRec.Code, moveRec.Body.String())
	}
	var placed string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM agent WHERE id = $1
	`, agentID).Scan(&placed); err != nil {
		t.Fatalf("load agent unit: %v", err)
	}
	if placed != unitID {
		t.Fatalf("agent unit = %q, want %q", placed, unitID)
	}

	clearRec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(clearRec, orgRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{"unit_id": ""}))
	if clearRec.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204: %s", clearRec.Code, clearRec.Body.String())
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM agent WHERE id = $1
	`, agentID).Scan(&placed); err != nil {
		t.Fatalf("load cleared agent unit: %v", err)
	}
	if placed != "" {
		t.Fatalf("agent unit after clear = %q, want empty", placed)
	}

	missingRec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(missingRec, orgRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{
		"unit_id": "00000000-0000-0000-0000-000000000000",
	}))
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("unknown unit status = %d, want 404: %s", missingRec.Code, missingRec.Body.String())
	}

	for user, want := range map[string]bool{admin: true, member: false} {
		list := httptest.NewRecorder()
		testHandler.ListOrgUnits(list, newRequestAs(user, "GET", "/api/org/units", nil))
		if list.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
		}
		var tree orgTreeResponse
		if err := json.Unmarshal(list.Body.Bytes(), &tree); err != nil {
			t.Fatalf("decode tree: %v", err)
		}
		if tree.CanManage != want {
			t.Fatalf("can_manage = %v, want %v", tree.CanManage, want)
		}
	}
}
