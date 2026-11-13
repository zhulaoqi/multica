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

func orgRequestWithParam(userID, method, path, name, value string, body any) *http.Request {
	req := newRequestAs(userID, method, path, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func orgRequestWithAgent(userID, method, path, agentID string, body any) *http.Request {
	return orgRequestWithParam(userID, method, path, "agentId", agentID, body)
}

func TestSetOrgAgentUnit_OwnerPlacesOwnTwinInOwnDepartment(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-twin-admin@example.com")
	member := createPermissionTestMember(t, "org-twin-member@example.com")
	twin := insertIntakeDutyAgent(t, "org-twin-agent", member)
	foreign := insertIntakeDutyAgent(t, "org-twin-foreign", admin)

	rec := httptest.NewRecorder()
	testHandler.ImportOrgUnits(rec, newRequestAs(admin, "POST", "/api/org/units/import", map[string]any{
		"rows": []map[string]any{{"path": "研发部/开发一组"}, {"path": "研发部/开发二组"}},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", rec.Code, rec.Body.String())
	}
	units := map[string]string{}
	rows, err := testPool.Query(context.Background(), `
		SELECT name, id FROM org_unit WHERE workspace_id = $1 AND name IN ('开发一组', '开发二组')
	`, testWorkspaceID)
	if err != nil {
		t.Fatalf("load units: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			t.Fatalf("scan unit: %v", err)
		}
		units[name] = id
	}
	ownUnit, sisterUnit := units["开发一组"], units["开发二组"]
	if ownUnit == "" || sisterUnit == "" {
		t.Fatalf("units = %v, want both development groups", units)
	}

	place := httptest.NewRecorder()
	testHandler.SetOrgMemberUnit(place, orgRequestWithParam(admin, "PUT", "/api/org/members/x/unit", "userId", member, map[string]any{"unit_id": ownUnit}))
	if place.Code != http.StatusNoContent {
		t.Fatalf("place member status = %d: %s", place.Code, place.Body.String())
	}

	// Registering a twin must not require lead rights: the owner files their
	// own agent into the department they belong to.
	own := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(own, orgRequestWithAgent(member, "PUT", "/api/org/agents/x/unit", twin, map[string]any{"unit_id": ownUnit}))
	if own.Code != http.StatusNoContent {
		t.Fatalf("owner twin placement status = %d, want 204: %s", own.Code, own.Body.String())
	}
	var placed string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM agent WHERE id = $1
	`, twin).Scan(&placed); err != nil {
		t.Fatalf("load twin unit: %v", err)
	}
	if placed != ownUnit {
		t.Fatalf("twin unit = %q, want %q", placed, ownUnit)
	}

	// The owner still cannot reach beyond their own department.
	sister := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(sister, orgRequestWithAgent(member, "PUT", "/api/org/agents/x/unit", twin, map[string]any{"unit_id": sisterUnit}))
	if sister.Code != http.StatusForbidden {
		t.Fatalf("owner sister placement status = %d, want 403: %s", sister.Code, sister.Body.String())
	}

	// Nor can they place someone else's agent, even inside their department.
	foreignRec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(foreignRec, orgRequestWithAgent(member, "PUT", "/api/org/agents/x/unit", foreign, map[string]any{"unit_id": ownUnit}))
	if foreignRec.Code != http.StatusForbidden {
		t.Fatalf("foreign agent placement status = %d, want 403: %s", foreignRec.Code, foreignRec.Body.String())
	}
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

func TestOrgUnitCrud_CreateRenameLeadDelete(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-crud-admin@example.com")
	member := createPermissionTestMember(t, "org-crud-member@example.com")
	agentID := insertIntakeDutyAgent(t, "org-crud-agent", admin)

	create := func(body map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.CreateOrgUnit(rec, newRequestAs(admin, "POST", "/api/org/units", body))
		return rec
	}

	rootRec := create(map[string]any{"name": " 研发中心 "})
	if rootRec.Code != http.StatusCreated {
		t.Fatalf("create root status = %d: %s", rootRec.Code, rootRec.Body.String())
	}
	var root struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rootRec.Body.Bytes(), &root); err != nil || root.ID == "" {
		t.Fatalf("decode created root: %v %s", err, rootRec.Body.String())
	}

	childRec := create(map[string]any{"name": "支付组", "parent_id": root.ID, "lead_user_id": member})
	if childRec.Code != http.StatusCreated {
		t.Fatalf("create child status = %d: %s", childRec.Code, childRec.Body.String())
	}
	var child struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(childRec.Body.Bytes(), &child); err != nil || child.ID == "" {
		t.Fatalf("decode created child: %v %s", err, childRec.Body.String())
	}

	if dup := create(map[string]any{"name": "支付组", "parent_id": root.ID}); dup.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409: %s", dup.Code, dup.Body.String())
	}
	if bad := create(map[string]any{"name": "  "}); bad.Code != http.StatusBadRequest {
		t.Fatalf("blank name status = %d, want 400", bad.Code)
	}
	if missing := create(map[string]any{"name": "孤儿组", "parent_id": "00000000-0000-0000-0000-000000000000"}); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown parent status = %d, want 404", missing.Code)
	}

	patch := func(unitID string, body map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.UpdateOrgUnit(rec, orgRequestWithParam(admin, "PATCH", "/api/org/units/x", "unitId", unitID, body))
		return rec
	}
	if rec := patch(child.ID, map[string]any{"name": "结算组"}); rec.Code != http.StatusNoContent {
		t.Fatalf("rename status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := patch(child.ID, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status = %d, want 400", rec.Code)
	}

	var strangerID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('stranger', 'org-crud-stranger@example.com') RETURNING id
	`).Scan(&strangerID); err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, strangerID) })
	if rec := patch(child.ID, map[string]any{"lead_user_id": strangerID}); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-member lead status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := patch(child.ID, map[string]any{"lead_user_id": ""}); rec.Code != http.StatusNoContent {
		t.Fatalf("clear lead status = %d: %s", rec.Code, rec.Body.String())
	}

	// A parent with children cannot disappear; the child can, and its agents
	// become unassigned rather than losing their workspace membership.
	del := func(unitID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.DeleteOrgUnit(rec, orgRequestWithParam(admin, "DELETE", "/api/org/units/x", "unitId", unitID, nil))
		return rec
	}
	if rec := del(root.ID); rec.Code != http.StatusConflict {
		t.Fatalf("delete parent with child status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	move := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(move, orgRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{"unit_id": child.ID}))
	if move.Code != http.StatusNoContent {
		t.Fatalf("place agent status = %d: %s", move.Code, move.Body.String())
	}
	if rec := del(child.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("delete child status = %d: %s", rec.Code, rec.Body.String())
	}
	var placed string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM agent WHERE id = $1
	`, agentID).Scan(&placed); err != nil {
		t.Fatalf("load agent unit after delete: %v", err)
	}
	if placed != "" {
		t.Fatalf("agent unit after unit delete = %q, want empty", placed)
	}
	if rec := del(root.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("delete root status = %d: %s", rec.Code, rec.Body.String())
	}

	for name, call := range map[string]func() *httptest.ResponseRecorder{
		"create": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			testHandler.CreateOrgUnit(rec, newRequestAs(member, "POST", "/api/org/units", map[string]any{"name": "成员建的组"}))
			return rec
		},
		"update": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			testHandler.UpdateOrgUnit(rec, orgRequestWithParam(member, "PATCH", "/api/org/units/x", "unitId", "00000000-0000-0000-0000-000000000000", map[string]any{"name": "x"}))
			return rec
		},
		"delete": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			testHandler.DeleteOrgUnit(rec, orgRequestWithParam(member, "DELETE", "/api/org/units/x", "unitId", "00000000-0000-0000-0000-000000000000", nil))
			return rec
		},
	} {
		if rec := call(); rec.Code != http.StatusForbidden {
			t.Fatalf("member %s status = %d, want 403", name, rec.Code)
		}
	}
}

func TestOrgMemberUnitAndUnitLeadRights(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-lead-rights-admin@example.com")
	lead := createPermissionTestMember(t, "org-lead-rights-lead@example.com")
	other := createPermissionTestMember(t, "org-lead-rights-other@example.com")

	call := func(userID, method, path, paramName, paramValue string, body any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.SetOrgMemberUnit(rec, orgRequestWithParam(userID, method, path, paramName, paramValue, body))
		return rec
	}
	patchUnit := func(userID, unitID string, body map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.UpdateOrgUnit(rec, orgRequestWithParam(userID, "PATCH", "/api/org/units/x", "unitId", unitID, body))
		return rec
	}
	createUnit := func(userID string, body map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.CreateOrgUnit(rec, newRequestAs(userID, "POST", "/api/org/units", body))
		return rec
	}
	deleteUnit := func(userID, unitID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.DeleteOrgUnit(rec, orgRequestWithParam(userID, "DELETE", "/api/org/units/x", "unitId", unitID, nil))
		return rec
	}

	rootRec := createUnit(admin, map[string]any{"name": "研发中心", "lead_user_id": lead})
	if rootRec.Code != http.StatusCreated {
		t.Fatalf("create root status = %d: %s", rootRec.Code, rootRec.Body.String())
	}
	var root struct{ ID string }
	if err := json.Unmarshal(rootRec.Body.Bytes(), &root); err != nil {
		t.Fatalf("decode root: %v", err)
	}

	// A lead creates children under their own unit but not sibling roots.
	childRec := createUnit(lead, map[string]any{"name": "支付组", "parent_id": root.ID})
	if childRec.Code != http.StatusCreated {
		t.Fatalf("lead create child status = %d: %s", childRec.Code, childRec.Body.String())
	}
	var child struct{ ID string }
	if err := json.Unmarshal(childRec.Body.Bytes(), &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	rootAgain := createUnit(lead, map[string]any{"name": "另一个部"})
	if rootAgain.Code != http.StatusForbidden {
		t.Fatalf("lead create root status = %d, want 403: %s", rootAgain.Code, rootAgain.Body.String())
	}

	// Non-lead members cannot place or rename anything.
	if rec := call(other, "PUT", "/api/org/members/x/unit", "userId", other, map[string]any{"unit_id": child.ID}); rec.Code != http.StatusForbidden {
		t.Fatalf("member place status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := patchUnit(other, child.ID, map[string]any{"name": "改名组"}); rec.Code != http.StatusForbidden {
		t.Fatalf("member rename status = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	// The lead moves a member in and out of their subtree, then renames.
	if rec := call(lead, "PUT", "/api/org/members/x/unit", "userId", other, map[string]any{"unit_id": child.ID}); rec.Code != http.StatusNoContent {
		t.Fatalf("lead place status = %d: %s", rec.Code, rec.Body.String())
	}
	var placed string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, other).Scan(&placed); err != nil {
		t.Fatalf("load member unit: %v", err)
	}
	if placed != child.ID {
		t.Fatalf("member unit = %q, want %q", placed, child.ID)
	}
	if rec := patchUnit(lead, child.ID, map[string]any{"name": "收单组"}); rec.Code != http.StatusNoContent {
		t.Fatalf("lead rename status = %d: %s", rec.Code, rec.Body.String())
	}

	// The tree reports per-unit manage rights: the lead manages the subtree,
	// a plain member manages nothing.
	tree := httptest.NewRecorder()
	testHandler.ListOrgUnits(tree, newRequestAs(lead, "GET", "/api/org/units", nil))
	if tree.Code != http.StatusOK {
		t.Fatalf("tree status = %d: %s", tree.Code, tree.Body.String())
	}
	var treeData orgTreeResponse
	if err := json.Unmarshal(tree.Body.Bytes(), &treeData); err != nil {
		t.Fatalf("decode tree: %v", err)
	}
	if treeData.CanManage {
		t.Fatal("workspace-level can_manage = true for a unit lead, want false")
	}
	placedMember := ""
	for _, entry := range treeData.Members {
		if entry.UserID == other {
			placedMember = entry.UnitID
		}
	}
	if placedMember != child.ID {
		t.Fatalf("tree member unit = %q, want %q: %+v", placedMember, child.ID, treeData.Members)
	}
	byID := map[string]orgUnitResponse{}
	for _, unit := range treeData.Units {
		byID[unit.ID] = unit
	}
	if !byID[child.ID].CanManage || !byID[root.ID].CanManage {
		t.Fatalf("lead can_manage child=%v root=%v, want true/true", byID[child.ID].CanManage, byID[root.ID].CanManage)
	}

	// Deleting a unit that still has members is refused.
	if rec := deleteUnit(admin, child.ID); rec.Code != http.StatusConflict {
		t.Fatalf("delete occupied status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// The member (no manage rights) cannot remove themselves either.
	if rec := call(other, "PUT", "/api/org/members/x/unit", "userId", other, map[string]any{"unit_id": ""}); rec.Code != http.StatusForbidden {
		t.Fatalf("member self-remove status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := call(lead, "PUT", "/api/org/members/x/unit", "userId", other, map[string]any{"unit_id": ""}); rec.Code != http.StatusNoContent {
		t.Fatalf("lead remove status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := deleteUnit(lead, child.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("lead delete child status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := deleteUnit(admin, root.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete root status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOrgSquadsAttachToUnitsAndMembersSeeTheirOwnDepartment(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-squad-admin@example.com")
	member := createPermissionTestMember(t, "org-squad-member@example.com")
	leaderAgent := insertIntakeDutyAgent(t, "org-squad-leader", admin)
	workerAgent := insertIntakeDutyAgent(t, "org-squad-worker", admin)

	var squadID string
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, '支付小队', '', $2, $3) RETURNING id
	`, testWorkspaceID, leaderAgent, admin).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2), ($1, 'agent', $3)
	`, squadID, leaderAgent, workerAgent); err != nil {
		t.Fatalf("add squad agents: %v", err)
	}

	createUnit := func(name string, parentID ...string) string {
		body := map[string]any{"name": name}
		if len(parentID) > 0 && parentID[0] != "" {
			body["parent_id"] = parentID[0]
		}
		rec := httptest.NewRecorder()
		testHandler.CreateOrgUnit(rec, newRequestAs(admin, "POST", "/api/org/units", body))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create unit %s status = %d: %s", name, rec.Code, rec.Body.String())
		}
		var created struct{ ID string }
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
			t.Fatalf("decode created unit: %v %s", err, rec.Body.String())
		}
		return created.ID
	}
	deleteUnit := func(unitID string) int {
		rec := httptest.NewRecorder()
		testHandler.DeleteOrgUnit(rec, orgRequestWithParam(admin, "DELETE", "/api/org/units/x", "unitId", unitID, nil))
		return rec.Code
	}
	unitID := createUnit("支付组")
	childID := createUnit("支付一组", unitID)
	createUnit("销售组")

	squadReq := func(userID, squad string, body any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.SetOrgSquadUnit(rec, orgRequestWithParam(userID, "PUT", "/api/org/squads/x/unit", "squadId", squad, body))
		return rec
	}
	// A plain member cannot attach anything; the admin can.
	if rec := squadReq(member, squadID, map[string]any{"unit_id": childID}); rec.Code != http.StatusForbidden {
		t.Fatalf("member squad attach status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := squadReq(admin, squadID, map[string]any{"unit_id": childID}); rec.Code != http.StatusNoContent {
		t.Fatalf("admin squad attach status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// The member's twin agent sits in their own department so the org view can
	// bind it back to them through owner_id.
	memberTwin := insertIntakeDutyAgent(t, "org-squad-member-twin", member)
	twinAttach := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(twinAttach, orgRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", memberTwin, map[string]any{"unit_id": childID}))
	if twinAttach.Code != http.StatusNoContent {
		t.Fatalf("admin twin attach status = %d, want 204: %s", twinAttach.Code, twinAttach.Body.String())
	}

	// The member placed in a sub-department sees that department and its
	// children only: no parent department and no sibling branches.
	place := httptest.NewRecorder()
	testHandler.SetOrgMemberUnit(place, orgRequestWithParam(admin, "PUT", "/api/org/members/x/unit", "userId", member, map[string]any{"unit_id": childID}))
	if place.Code != http.StatusNoContent {
		t.Fatalf("place member status = %d: %s", place.Code, place.Body.String())
	}
	list := httptest.NewRecorder()
	testHandler.ListOrgUnits(list, newRequestAs(member, "GET", "/api/org/units", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
	}
	var tree orgTreeResponse
	if err := json.Unmarshal(list.Body.Bytes(), &tree); err != nil {
		t.Fatalf("decode tree: %v", err)
	}
	if tree.CanManage {
		t.Fatal("member can_manage = true, want false")
	}
	if tree.MyUnitID != childID {
		t.Fatalf("my_unit_id = %q, want %q", tree.MyUnitID, childID)
	}
	unitNames := map[string]string{}
	for _, unit := range tree.Units {
		unitNames[unit.ID] = unit.Name
	}
	if len(tree.Units) != 1 || unitNames[childID] != "支付一组" {
		t.Fatalf("member tree = %v, want only own department", unitNames)
	}
	if len(tree.Squads) != 1 {
		t.Fatalf("squads = %+v, want one", tree.Squads)
	}
	squad := tree.Squads[0]
	if squad.ID != squadID || squad.UnitID != childID || squad.AgentCount != 2 {
		t.Fatalf("squad = %+v, want id %q unit %q with 2 agents", squad, squadID, childID)
	}
	// The member sees exactly their own twin, bound to them by owner_id with
	// its responsibility text; other agents stay out of a plain member's view.
	if len(tree.Agents) != 1 {
		t.Fatalf("member agents = %+v, want only the own twin", tree.Agents)
	}
	if twin := tree.Agents[0]; twin.ID != memberTwin || twin.OwnerID != member || twin.Can == "" {
		t.Fatalf("member twin = %+v, want id %s owned by the member with a responsibility", twin, memberTwin)
	}
	// The admin still sees everything, including each agent's availability.
	adminList := httptest.NewRecorder()
	testHandler.ListOrgUnits(adminList, newRequestAs(admin, "GET", "/api/org/units", nil))
	if adminList.Code != http.StatusOK {
		t.Fatalf("admin list status = %d: %s", adminList.Code, adminList.Body.String())
	}
	var adminTree orgTreeResponse
	if err := json.Unmarshal(adminList.Body.Bytes(), &adminTree); err != nil {
		t.Fatalf("decode admin tree: %v", err)
	}
	if len(adminTree.Units) != 3 {
		t.Fatalf("admin tree has %d units, want 3", len(adminTree.Units))
	}
	statuses := map[string]string{}
	owners := map[string]string{}
	for _, agent := range adminTree.Agents {
		statuses[agent.Name] = agent.Status
		owners[agent.Name] = agent.OwnerID
	}
	if statuses["org-squad-leader"] == "" {
		t.Fatalf("agent statuses = %v, want a status for every agent", statuses)
	}
	if owners["org-squad-leader"] != admin || owners["org-squad-member-twin"] != member {
		t.Fatalf("agent owners = %v, want squad leader owned by admin and twin owned by the member", owners)
	}

	// A member outside every department sees an empty organisation.
	clearPlace := httptest.NewRecorder()
	testHandler.SetOrgMemberUnit(clearPlace, orgRequestWithParam(admin, "PUT", "/api/org/members/x/unit", "userId", member, map[string]any{"unit_id": ""}))
	if clearPlace.Code != http.StatusNoContent {
		t.Fatalf("clear member status = %d: %s", clearPlace.Code, clearPlace.Body.String())
	}
	detached := httptest.NewRecorder()
	testHandler.ListOrgUnits(detached, newRequestAs(member, "GET", "/api/org/units", nil))
	var detachedTree orgTreeResponse
	if err := json.Unmarshal(detached.Body.Bytes(), &detachedTree); err != nil {
		t.Fatalf("decode detached tree: %v", err)
	}
	if len(detachedTree.Units) != 0 || len(detachedTree.Squads) != 0 || detachedTree.MyUnitID != "" {
		t.Fatalf("detached member tree = %+v, want empty", detachedTree)
	}

	// Deleting the unit detaches the squad instead of leaving a dangling id.
	if code := deleteUnit(childID); code != http.StatusNoContent {
		t.Fatalf("delete child unit status = %d", code)
	}
	if code := deleteUnit(unitID); code != http.StatusNoContent {
		t.Fatalf("delete unit status = %d", code)
	}
	var squadUnit string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM squad WHERE id = $1
	`, squadID).Scan(&squadUnit); err != nil {
		t.Fatalf("load squad unit: %v", err)
	}
	if squadUnit != "" {
		t.Fatalf("squad unit after delete = %q, want empty", squadUnit)
	}
}
