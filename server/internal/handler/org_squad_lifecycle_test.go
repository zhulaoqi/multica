package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func updateOrgUnitLead(t *testing.T, admin, unitID, leadID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.UpdateOrgUnit(rec, operatorRequestWithParam(admin, "PATCH", "/api/org/units/x", "unitId", unitID, map[string]any{
		"lead_user_id": leadID,
	}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update unit lead status = %d: %s", rec.Code, rec.Body.String())
	}
}

func moveOrgMember(t *testing.T, admin, userID, unitID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.SetOrgMemberUnit(rec, operatorRequestWithParam(admin, "PUT", "/api/org/members/x/unit", "userId", userID, map[string]any{
		"unit_id": unitID,
	}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("move member status = %d: %s", rec.Code, rec.Body.String())
	}
}

func deleteMemberRequest(userID, workspaceID, memberID string) *http.Request {
	req := operatorRequest(userID, "DELETE", "/api/workspaces/x/members/y", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workspaceID)
	rctx.URLParams.Add("memberId", memberID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func squadLeader(t *testing.T, unitID string) string {
	t.Helper()
	var leader string
	if err := testPool.QueryRow(context.Background(), `
		SELECT squad.leader_id FROM squad
		JOIN org_unit ON org_unit.id = squad.org_unit_id
		WHERE org_unit.id = $1 AND squad.archived_at IS NULL
	`, unitID).Scan(&leader); err != nil {
		t.Fatalf("load squad leader: %v", err)
	}
	return leader
}

func agentOrgUnit(t *testing.T, agentID string) string {
	t.Helper()
	var unit string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(org_unit_id::text, '') FROM agent WHERE id = $1`, agentID).Scan(&unit); err != nil {
		t.Fatalf("load agent unit: %v", err)
	}
	return unit
}

// TestUpdateOrgUnitLead_RotatesSquadToNewLeadTwin covers the lead handover: the
// new department lead's primary twin takes the squad over when it can run.
func TestUpdateOrgUnitLead_RotatesSquadToNewLeadTwin(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "lifecycle-admin@example.com")
	leadA := createPermissionTestMember(t, "lifecycle-lead-a@example.com")
	leadB := createPermissionTestMember(t, "lifecycle-lead-b@example.com")
	agentA := insertRunnableDutyAgent(t, "lifecycle-agent-a", leadA)
	agentB := insertRunnableDutyAgent(t, "lifecycle-agent-b", leadB)
	unit := createOrgUnitAs(t, admin, "生命周期交接部", "")
	placeOrgAgent(t, admin, agentA, unit)
	placeOrgAgent(t, admin, agentB, unit)
	setOrgUnitLead(t, unit, leadA)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res := generateOrgUnitSquad(t, admin, true, unit, map[string]any{
		"name":      "生命周期交接小队",
		"leader_id": agentA,
	})
	if status != http.StatusCreated {
		t.Fatalf("generate squad status = %d, want 201: %+v", status, res)
	}
	if got := squadLeader(t, unit); got != agentA {
		t.Fatalf("initial leader = %s, want %s", got, agentA)
	}

	updateOrgUnitLead(t, admin, unit, leadB)

	if got := squadLeader(t, unit); got != agentB {
		t.Fatalf("leader after handover = %s, want the new lead's twin %s", got, agentB)
	}
	var role string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role FROM squad_member WHERE squad_id = $1 AND member_id = $2
	`, res.Squad.ID, agentA).Scan(&role); err != nil {
		t.Fatalf("load previous leader row: %v", err)
	}
	if role == "leader" {
		t.Fatalf("previous leader still carries the reserved leader role")
	}
}

// TestSetMemberUnit_MovesPrimaryTwin covers a member transfer: the primary
// twin that represents them follows them into the new department.
func TestSetMemberUnit_MovesPrimaryTwin(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "lifecycle-move-admin@example.com")
	member := createPermissionTestMember(t, "lifecycle-move-member@example.com")
	twin := insertRunnableDutyAgent(t, "lifecycle-move-twin", member)
	unitA := createOrgUnitAs(t, admin, "调动前部门", "")
	unitB := createOrgUnitAs(t, admin, "调动后部门", "")
	placeOrgMember(t, member, unitA)
	placeOrgAgent(t, admin, twin, unitA)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	moveOrgMember(t, admin, member, unitB)

	if got := agentOrgUnit(t, twin); got != unitB {
		t.Fatalf("primary twin unit = %s, want %s", got, unitB)
	}
}

// TestMemberDeparture_RotatesAwayFromArchivedLeader covers a departure: the
// removed member's agent is archived but may still be the squad leader, so the
// next online agent in the department takes over.
func TestMemberDeparture_RotatesAwayFromArchivedLeader(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "lifecycle-leave-admin@example.com")
	leaver := createPermissionTestMember(t, "lifecycle-leave-user@example.com")
	leaderAgent := insertRunnableDutyAgent(t, "lifecycle-leave-leader", leaver)
	workerAgent := insertRunnableDutyAgent(t, "lifecycle-leave-worker", admin)
	unit := createOrgUnitAs(t, admin, "离职交接部", "")
	placeOrgAgent(t, admin, leaderAgent, unit)
	placeOrgAgent(t, admin, workerAgent, unit)
	placeOrgMember(t, leaver, unit)
	setOrgUnitLead(t, unit, leaver)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res := generateOrgUnitSquad(t, admin, true, unit, map[string]any{
		"name":      "离职交接小队",
		"leader_id": leaderAgent,
	})
	if status != http.StatusCreated {
		t.Fatalf("generate squad status = %d, want 201: %+v", status, res)
	}

	var memberID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, leaver).Scan(&memberID); err != nil {
		t.Fatalf("load member row: %v", err)
	}
	rec := httptest.NewRecorder()
	testHandler.DeleteMember(rec, deleteMemberRequest(admin, testWorkspaceID, memberID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete member status = %d: %s", rec.Code, rec.Body.String())
	}

	if got := squadLeader(t, unit); got != workerAgent {
		t.Fatalf("leader after departure = %s, want the remaining online agent %s", got, workerAgent)
	}
	var role string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role FROM squad_member WHERE squad_id = $1 AND member_id = $2
	`, res.Squad.ID, leaderAgent).Scan(&role); err != nil {
		t.Fatalf("load departed leader row: %v", err)
	}
	if role == "leader" {
		t.Fatalf("departed leader still carries the reserved leader role")
	}
}
