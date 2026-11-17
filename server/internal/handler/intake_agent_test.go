package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntakeAgent_EmptyCanDoesNotCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count agents: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(w, newRequest("POST", "/api/intake/agents", map[string]any{
		"name":         "intake-empty-can",
		"can":          "   ",
		"runtime_mode": "local",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if after != before {
		t.Fatalf("agent count = %d, want %d", after, before)
	}
}

func TestIntakeAgent_RegisterAndListOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ownerID := createPermissionTestMember(t, "intake-agent-owner@example.com")
	otherID := createPermissionTestMember(t, "intake-agent-other@example.com")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/intake/agents", map[string]any{
		"name":         "intake-refund",
		"can":          "退款",
		"cannot":       "登录",
		"runtime_mode": "local",
	})
	req.Header.Set("X-User-ID", ownerID)
	testHandler.CreateIntakeAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Can         string `json:"can"`
		Cannot      string `json:"cannot"`
		RuntimeMode string `json:"runtime_mode"`
		Online      bool   `json:"online"`
		OwnerID     string `json:"owner_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})
	if created.OwnerID != ownerID || created.Can != "退款" || created.Cannot != "登录" || created.Online {
		t.Fatalf("created = %+v", created)
	}

	list := httptest.NewRecorder()
	listReq := newRequest("GET", "/api/intake/agents", nil)
	listReq.Header.Set("X-User-ID", ownerID)
	testHandler.ListIntakeAgents(list, listReq)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
	}
	var rows []struct {
		ID     string `json:"id"`
		Online bool   `json:"online"`
	}
	if err := json.NewDecoder(list.Body).Decode(&rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner list = %+v, missing %s", rows, created.ID)
	}

	other := httptest.NewRecorder()
	otherReq := newRequest("GET", "/api/intake/agents", nil)
	otherReq.Header.Set("X-User-ID", otherID)
	testHandler.ListIntakeAgents(other, otherReq)
	if other.Code != http.StatusOK {
		t.Fatalf("other list status = %d: %s", other.Code, other.Body.String())
	}
	var otherRows []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(other.Body).Decode(&otherRows); err != nil {
		t.Fatalf("decode other: %v", err)
	}
	for _, row := range otherRows {
		if row.ID == created.ID {
			t.Fatalf("other user can see agent %s", created.ID)
		}
	}
}

func TestIntakeAgent_LeadConfiguresDirectReportTwin(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "intake-lead-admin@example.com")
	lead := createPermissionTestMember(t, "intake-lead-lead@example.com")
	report := createPermissionTestMember(t, "intake-lead-report@example.com")
	stranger := createPermissionTestMember(t, "intake-lead-stranger@example.com")

	importRec := httptest.NewRecorder()
	testHandler.ImportOrgUnits(importRec, operatorRequest(admin, "POST", "/api/org/units/import", map[string]any{
		"rows": []map[string]any{
			{"path": "研发部/开发一组", "lead_email": "intake-lead-lead@example.com"},
			{"path": "研发部/开发二组"},
		},
	}))
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", importRec.Code, importRec.Body.String())
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
	for userID, unitID := range map[string]string{report: ownUnit, stranger: sisterUnit} {
		place := httptest.NewRecorder()
		testHandler.SetOrgMemberUnit(place, operatorRequestWithParam(admin, "PUT", "/api/org/members/x/unit", "userId", userID, map[string]any{"unit_id": unitID}))
		if place.Code != http.StatusNoContent {
			t.Fatalf("place member status = %d: %s", place.Code, place.Body.String())
		}
	}

	// The lead registers a twin on behalf of a direct report.
	createRec := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(createRec, newRequestAs(lead, "POST", "/api/intake/agents", map[string]any{
		"name":         "intake-lead-twin",
		"can":          "写代码",
		"runtime_mode": "local",
		"owner_id":     report,
	}))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("lead create status = %d, want 201: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		OwnerID string `json:"owner_id"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})
	if created.OwnerID != report {
		t.Fatalf("created owner = %q, want report %q", created.OwnerID, report)
	}

	// The lead edits the report's twin and the owner binding survives.
	patchRec := httptest.NewRecorder()
	testHandler.PatchIntakeAgent(patchRec, orgRequestWithAgent(lead, "PATCH", "/api/intake/agents/x", created.ID, map[string]any{
		"name":         "intake-lead-twin-v2",
		"can":          "写代码、做评审",
		"runtime_mode": "local",
	}))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("lead patch status = %d, want 200: %s", patchRec.Code, patchRec.Body.String())
	}
	var patched struct {
		Name    string `json:"name"`
		Can     string `json:"can"`
		OwnerID string `json:"owner_id"`
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched: %v", err)
	}
	if patched.OwnerID != report || patched.Name != "intake-lead-twin-v2" || patched.Can != "写代码、做评审" {
		t.Fatalf("patched = %+v", patched)
	}

	// The lead cannot register for a member of a department they do not lead.
	strangerRec := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(strangerRec, newRequestAs(lead, "POST", "/api/intake/agents", map[string]any{
		"name":         "intake-lead-stranger",
		"can":          "写代码",
		"runtime_mode": "local",
		"owner_id":     stranger,
	}))
	if strangerRec.Code != http.StatusForbidden {
		t.Fatalf("lead stranger create status = %d, want 403: %s", strangerRec.Code, strangerRec.Body.String())
	}

	// A plain member cannot register on someone else's behalf either.
	memberRec := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(memberRec, newRequestAs(report, "POST", "/api/intake/agents", map[string]any{
		"name":         "intake-lead-onbehalf",
		"can":          "写代码",
		"runtime_mode": "local",
		"owner_id":     lead,
	}))
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member on-behalf create status = %d, want 403: %s", memberRec.Code, memberRec.Body.String())
	}
}
