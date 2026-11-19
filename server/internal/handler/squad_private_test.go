package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// createPrivateSquadAs creates a personal squad through the same handler the
// client uses and registers cleanup for the squad and its members.
func createPrivateSquadAs(t *testing.T, userID, name, leaderID string) SquadResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, squadScopeReq(userID, "POST", "/api/squads", map[string]any{
		"name":       name,
		"leader_id":  leaderID,
		"is_private": true,
	}, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad: %v", err)
	}
	if !resp.IsPrivate {
		t.Fatalf("squad is_private = false, want true")
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
	return resp
}

func listSquadsAs(t *testing.T, userID string) []SquadResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListSquads(w, squadScopeReq(userID, "GET", "/api/squads", nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListSquads: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squads: %v", err)
	}
	return resp
}

func squadIDs(resp []SquadResponse) map[string]struct{} {
	ids := make(map[string]struct{}, len(resp))
	for _, squad := range resp {
		ids[squad.ID] = struct{}{}
	}
	return ids
}

// TestListSquads_PersonalSquadVisibleOnlyToCreator covers the personal-squad
// visibility rule: not even a workspace admin sees someone else's squad.
func TestListSquads_PersonalSquadVisibleOnlyToCreator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-personal-creator@multica.test")
	strangerID := createPlainMember(t, "squad-personal-stranger@multica.test")
	adminID := createOrgTestAdmin(t, "squad-personal-admin@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-personal-leader", nil)

	squad := createPrivateSquadAs(t, creatorID, "Creator Personal Squad", leaderID)

	if _, ok := squadIDs(listSquadsAs(t, creatorID))[squad.ID]; !ok {
		t.Fatal("creator does not see their own personal squad")
	}
	if _, ok := squadIDs(listSquadsAs(t, strangerID))[squad.ID]; ok {
		t.Fatal("plain member sees someone else's personal squad")
	}
	if _, ok := squadIDs(listSquadsAs(t, adminID))[squad.ID]; ok {
		t.Fatal("admin sees someone else's personal squad")
	}
}

// TestPersonalSquad_OnlyCreatorCanOpenOrManage keeps read and manage gates in
// line with list visibility, including for workspace admins.
func TestPersonalSquad_OnlyCreatorCanOpenOrManage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-personal-open-creator@multica.test")
	adminID := createOrgTestAdmin(t, "squad-personal-open-admin@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-personal-open-leader", nil)
	squad := createPrivateSquadAs(t, creatorID, "Open Personal Squad", leaderID)
	params := map[string]string{"id": squad.ID}

	w := httptest.NewRecorder()
	testHandler.GetSquad(w, squadScopeReq(creatorID, "GET", "/api/squads", nil, params))
	if w.Code != http.StatusOK {
		t.Fatalf("creator GetSquad: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.GetSquad(w, squadScopeReq(adminID, "GET", "/api/squads", nil, params))
	if w.Code != http.StatusNotFound {
		t.Fatalf("admin GetSquad: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(adminID, "PATCH", "/api/squads", map[string]any{
		"name": "Admin Tried To Rename",
	}, params))
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin UpdateSquad: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateSquad_SecondPersonalSquadConflicts keeps one live personal squad
// per creator so the workbench entry always has a single target.
func TestCreateSquad_SecondPersonalSquadConflicts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-personal-dupe@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-personal-dupe-leader", nil)
	createPrivateSquadAs(t, creatorID, "First Personal Squad", leaderID)

	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, squadScopeReq(creatorID, "POST", "/api/squads", map[string]any{
		"name":       "Second Personal Squad",
		"leader_id":  leaderID,
		"is_private": true,
	}, nil))
	if w.Code != http.StatusConflict || !containsCode(w.Body.String(), "squad_personal_exists") {
		t.Fatalf("second personal squad: expected 409 squad_personal_exists, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListOrgUnits_HidesPersonalSquads keeps personal squads out of the
// department structure for everyone, admins included.
func TestListOrgUnits_HidesPersonalSquads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	adminID := createOrgTestAdmin(t, "squad-personal-org-admin@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-personal-org-leader", nil)
	shared := createSquadAs(t, adminID, "Org Shared Squad", leaderID)
	personal := createPrivateSquadAs(t, adminID, "Org Personal Squad", leaderID)

	// The operator sees every shared squad; that is the widest view the org
	// tree can return, so it is the one that must still exclude personal ones.
	w := httptest.NewRecorder()
	testHandler.ListOrgUnits(w, operatorRequest(adminID, "GET", "/api/org/units", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListOrgUnits: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp orgTreeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode org tree: %v", err)
	}
	ids := make(map[string]struct{}, len(resp.Squads))
	for _, squad := range resp.Squads {
		ids[squad.ID] = struct{}{}
	}
	if _, ok := ids[shared.ID]; !ok {
		t.Fatal("org tree is missing the shared squad")
	}
	if _, ok := ids[personal.ID]; ok {
		t.Fatal("org tree lists a personal squad")
	}
}

// TestValidateAssigneePair_PersonalSquadCreatesOnly covers the issue-assignment
// gate: only the creator may point an issue at their personal squad.
func TestValidateAssigneePair_PersonalSquadCreatesOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-personal-assign-creator@multica.test")
	strangerID := createPlainMember(t, "squad-personal-assign-stranger@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-personal-assign-leader", nil)
	squad := createPrivateSquadAs(t, creatorID, "Assign Personal Squad", leaderID)
	assignee := pgtype.Text{String: "squad", Valid: true}
	squadUUID := parseUUID(squad.ID)

	creatorReq := newRequestAs(creatorID, "POST", "/api/issues", nil)
	if status, msg := testHandler.validateAssigneePair(context.Background(), creatorReq, testWorkspaceID, assignee, squadUUID); status != 0 {
		t.Fatalf("creator assignee check = %d %q, want valid", status, msg)
	}
	strangerReq := newRequestAs(strangerID, "POST", "/api/issues", nil)
	if status, _ := testHandler.validateAssigneePair(context.Background(), strangerReq, testWorkspaceID, assignee, squadUUID); status != http.StatusBadRequest {
		t.Fatalf("stranger assignee check = %d, want 400", status)
	}
}
