package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createOrgUnitAs(t *testing.T, admin, name, parentID string) string {
	t.Helper()
	body := map[string]any{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	rec := httptest.NewRecorder()
	testHandler.CreateOrgUnit(rec, operatorRequest(admin, "POST", "/api/org/units", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create unit %s status = %d: %s", name, rec.Code, rec.Body.String())
	}
	var created struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode created unit: %v %s", err, rec.Body.String())
	}
	return created.ID
}

func placeOrgAgent(t *testing.T, admin, agentID, unitID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.SetOrgAgentUnit(rec, operatorRequestWithAgent(admin, "PUT", "/api/org/agents/x/unit", agentID, map[string]any{"unit_id": unitID}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("place agent %s status = %d: %s", agentID, rec.Code, rec.Body.String())
	}
}

func generateOrgUnitSquad(t *testing.T, userID string, operator bool, unitID string, body map[string]any) (int, orgUnitSquadResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := orgRequestWithParam(userID, "POST", "/api/org/units/x/squad", "unitId", unitID, body)
	if operator {
		req = operatorRequestWithParam(userID, "POST", "/api/org/units/x/squad", "unitId", unitID, body)
	}
	testHandler.GenerateOrgUnitSquad(rec, req)
	var res orgUnitSquadResponse
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode squad response: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res
}

func TestGenerateOrgUnitSquad_CreatesThenSyncsDepartmentRoster(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-gen-admin@example.com")
	leader := insertIntakeDutyAgent(t, "org-gen-leader", admin)
	worker := insertIntakeDutyAgent(t, "org-gen-worker", admin)
	outsider := insertIntakeDutyAgent(t, "org-gen-outsider", admin)

	root := createOrgUnitAs(t, admin, "研发中心", "")
	child := createOrgUnitAs(t, admin, "支付组", root)
	other := createOrgUnitAs(t, admin, "销售组", "")
	placeOrgAgent(t, admin, leader, root)
	placeOrgAgent(t, admin, worker, child)
	placeOrgAgent(t, admin, outsider, other)

	status, res := generateOrgUnitSquad(t, admin, true, root, map[string]any{
		"name":         "研发小队",
		"leader_id":    leader,
		"wip_limit":    2,
		"max_children": 5,
	})
	if status != http.StatusCreated {
		t.Fatalf("generate status = %d, want 201: %+v", status, res)
	}
	squadID := res.Squad.ID
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	if !res.Created {
		t.Fatal("created = false, want true on first generate")
	}
	if res.Added != 2 {
		t.Fatalf("added = %d, want leader and worker (%+v)", res.Added, res)
	}
	if res.Squad.Name != "研发小队" || res.Squad.UnitID != root || res.Squad.LeaderID != leader {
		t.Fatalf("squad = %+v, want name/unit/leader from the request", res.Squad)
	}
	if res.Squad.WipLimit != 2 || res.Squad.MaxChildren != 5 || res.Squad.AgentCount != 2 {
		t.Fatalf("squad limits/count = %+v, want wip 2, max 5, 2 agents", res.Squad)
	}
	if role := squadMemberRole(t, squadID, worker); role != "负责 org-gen-worker" {
		t.Fatalf("worker role = %q, want the agent's responsibility", role)
	}

	// A second call is a no-op sync, not a duplicate squad.
	status, res = generateOrgUnitSquad(t, admin, true, root, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("sync status = %d, want 200: %+v", status, res)
	}
	if res.Created || res.Added != 0 || res.Removed != 0 || res.Squad.ID != squadID {
		t.Fatalf("sync = %+v, want unchanged squad %q", res, squadID)
	}

	// A hand-added member outside the department survives the sync.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO squad_member (squad_id, member_type, member_id, source) VALUES ($1, 'agent', $2, 'manual')
	`, squadID, outsider); err != nil {
		t.Fatalf("add manual member: %v", err)
	}
	// Moving an agent out of the subtree re-syncs the department squad on the
	// spot; the manual row is not covered either but must survive.
	placeOrgAgent(t, admin, worker, other)
	if _, ok := findSquadMember(t, squadID, worker); ok {
		t.Fatal("worker that left the department is still a member")
	}
	if _, ok := findSquadMember(t, squadID, outsider); !ok {
		t.Fatal("manual member outside the department was pruned; it must survive syncs")
	}
	status, res = generateOrgUnitSquad(t, admin, true, root, map[string]any{})
	if status != http.StatusOK || res.Removed != 0 {
		t.Fatalf("follow-up sync = %d %+v, want a stable roster", status, res)
	}

	// Only a department manager may generate its squad, and only with a leader
	// agent the caller can wire into squads.
	member := createPermissionTestMember(t, "org-gen-member@example.com")
	if status, _ := generateOrgUnitSquad(t, member, false, root, map[string]any{"name": "越权小队", "leader_id": leader}); status != http.StatusForbidden {
		t.Fatalf("member generate status = %d, want 403", status)
	}
	if status, _ := generateOrgUnitSquad(t, admin, true, child, map[string]any{"name": "无名小队"}); status != http.StatusBadRequest {
		t.Fatalf("missing leader status = %d, want 400", status)
	}
}

func squadMemberRole(t *testing.T, squadID, agentID string) string {
	t.Helper()
	row, ok := findSquadMember(t, squadID, agentID)
	if !ok {
		return ""
	}
	return row
}

func findSquadMember(t *testing.T, squadID, agentID string) (string, bool) {
	t.Helper()
	var role string
	err := testPool.QueryRow(context.Background(), `
		SELECT role FROM squad_member WHERE squad_id = $1 AND member_type = 'agent' AND member_id = $2
	`, squadID, agentID).Scan(&role)
	if err != nil {
		return "", false
	}
	return role, true
}

func TestSquadChildGuard_LimitsAgentCreatedChildren(t *testing.T) {
	requireDB(t)
	admin := createOrgTestAdmin(t, "org-guard-admin@example.com")
	leader := insertIntakeDutyAgent(t, "org-guard-leader", admin)

	insertSquad := func(maxChildren, wipLimit int) string {
		var id string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO squad (workspace_id, name, leader_id, creator_id, max_children, wip_limit)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
		`, testWorkspaceID, fmt.Sprintf("护栏小队 %d/%d", maxChildren, wipLimit), leader, admin, maxChildren, wipLimit).Scan(&id); err != nil {
			t.Fatalf("insert squad: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, id) })
		return id
	}
	parentFor := func(squadID string) string {
		return dbfx.Issue(t, "squad parent", testutil.Cols{
			"assignee_type": "squad",
			"assignee_id":   squadID,
		})
	}

	wsUUID := parseUUID(testWorkspaceID)

	// An open child fills the single WIP slot, blocking the next dispatch.
	wipSquad := insertSquad(10, 1)
	wipParent := parentFor(wipSquad)
	if status, _ := testHandler.checkSquadChildGuard(context.Background(), wsUUID, parseUUID(wipParent)); status != 0 {
		t.Fatalf("empty parent guard status = %d, want 0", status)
	}
	dbfx.Issue(t, "open child", testutil.Cols{"parent_issue_id": wipParent, "status": "todo"})
	status, msg := testHandler.checkSquadChildGuard(context.Background(), wsUUID, parseUUID(wipParent))
	if status != http.StatusConflict || !strings.Contains(msg, "open child limit") {
		t.Fatalf("wip guard = %d %q, want 409 open child limit", status, msg)
	}

	// Closed children still count toward the total cap.
	totalSquad := insertSquad(1, 10)
	totalParent := parentFor(totalSquad)
	dbfx.Issue(t, "done child", testutil.Cols{"parent_issue_id": totalParent, "status": "done"})
	status, msg = testHandler.checkSquadChildGuard(context.Background(), wsUUID, parseUUID(totalParent))
	if status != http.StatusConflict || !strings.Contains(msg, "dispatch limit reached") {
		t.Fatalf("total guard = %d %q, want 409 dispatch limit", status, msg)
	}

	// A parent not assigned to a squad is never gated.
	plain := dbfx.Issue(t, "plain parent")
	dbfx.Issue(t, "plain child", testutil.Cols{"parent_issue_id": plain, "status": "todo"})
	if status, _ := testHandler.checkSquadChildGuard(context.Background(), wsUUID, parseUUID(plain)); status != 0 {
		t.Fatalf("non-squad parent guard status = %d, want 0", status)
	}
}

func TestNotifySquadLeaderFailure_NotifiesDepartmentLeadOnlyWhenExhausted(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-fail-admin@example.com")
	leader := insertIntakeDutyAgent(t, "org-fail-leader", admin)
	unitID := createOrgUnitAs(t, admin, "失败告警部", "")
	if _, err := testPool.Exec(context.Background(), `UPDATE org_unit SET lead_user_id = $2 WHERE id = $1`, unitID, admin); err != nil {
		t.Fatalf("set unit lead: %v", err)
	}
	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id, org_unit_id)
		VALUES ($1, '失败告警小队', $2, $3, $4) RETURNING id
	`, testWorkspaceID, leader, admin, unitID).Scan(&squadID); err != nil {
		t.Fatalf("insert squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })
	issueID := dbfx.Issue(t, "leader run")

	task := db.AgentTaskQueue{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		AgentID:      parseUUID(leader),
		IssueID:      parseUUID(issueID),
		SquadID:      parseUUID(squadID),
		IsLeaderTask: true,
		Attempt:      1,
		MaxAttempts:  3,
	}
	testHandler.notifySquadLeaderFailure(context.Background(), task, testWorkspaceID)
	if n := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = $2`, admin, inboxTypeSquadLeaderFailed); n != 0 {
		t.Fatalf("inbox items before attempts exhausted = %d, want 0", n)
	}

	task.Attempt = 3
	task.Error = pgtype.Text{String: "runtime crashed", Valid: true}
	testHandler.notifySquadLeaderFailure(context.Background(), task, testWorkspaceID)
	if n := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = $2`, admin, inboxTypeSquadLeaderFailed); n != 1 {
		t.Fatalf("inbox items after attempts exhausted = %d, want 1", n)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE recipient_id = $1 AND type = $2`, admin, inboxTypeSquadLeaderFailed)
	})
}
