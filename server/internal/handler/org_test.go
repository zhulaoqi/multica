package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
