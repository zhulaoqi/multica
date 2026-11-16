package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func dispatchToOrgUnit(t *testing.T, userID, unitID string, body map[string]any) (int, orgDispatchResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.DispatchToOrgUnit(rec, orgRequestWithParam(userID, "POST", "/api/org/units/x/dispatch", "unitId", unitID, body))
	var res orgDispatchResponse
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode dispatch response: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res, rec.Body.String()
}

func placeOrgMember(t *testing.T, userID, unitID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, userID, unitID); err != nil {
		t.Fatalf("place member %s in unit %s: %v", userID, unitID, err)
	}
}

func setOrgUnitLead(t *testing.T, unitID, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE org_unit SET lead_user_id = $2 WHERE id = $1
	`, unitID, userID); err != nil {
		t.Fatalf("set unit lead: %v", err)
	}
}

// startDispatchTest clears leftovers from earlier runs. Fixture cleanups delete
// users, and issues hold a non-cascading creator reference, so the test must
// also register clearDispatchFixtures(t) once its fixtures exist: cleanups run
// in reverse registration order, which makes the dispatch rows go first.
func startDispatchTest(t *testing.T) {
	t.Helper()
	requireDB(t)
	cleanupOrgUnits(t)
	clearDispatchFixtures(t)
}

func clearDispatchFixtures(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM dispatch_origin WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'dispatch_completed'`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch-test-%'`, testWorkspaceID)
	testPool.Exec(ctx, `
		DELETE FROM squad_member WHERE squad_id IN (
			SELECT id FROM squad WHERE workspace_id = $1 AND org_unit_id IS NOT NULL
		)
	`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM squad WHERE workspace_id = $1 AND org_unit_id IS NOT NULL`, testWorkspaceID)
}

func TestDispatchToOrgUnit_CreatesSquadIssueAndOrigin(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-admin@example.com")
	leader := insertIntakeDutyAgent(t, "dispatch-leader", admin)
	unitID := createOrgUnitAs(t, admin, "派活一部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title":       "dispatch-test 支付回调改造",
		"description": "## 目标\n改造回调",
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	if res.Issue.AssigneeType == nil || *res.Issue.AssigneeType != "squad" {
		t.Fatalf("assignee type = %v, want squad", res.Issue.AssigneeType)
	}
	var squadID, squadUnit, squadLeader string
	var orgManaged bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT id, org_managed, org_unit_id, leader_id FROM squad WHERE workspace_id = $1 AND org_unit_id = $2
	`, testWorkspaceID, unitID).Scan(&squadID, &orgManaged, &squadUnit, &squadLeader); err != nil {
		t.Fatalf("load auto-created squad: %v", err)
	}
	if !orgManaged || squadUnit != unitID {
		t.Fatalf("squad org_managed/unit = %v/%s, want true/%s", orgManaged, squadUnit, unitID)
	}
	if squadLeader != leader {
		t.Fatalf("squad leader = %s, want %s", squadLeader, leader)
	}
	if res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != squadID {
		t.Fatalf("assignee id = %v, want squad %s", res.Issue.AssigneeID, squadID)
	}

	var requester, targetUnit, channel string
	if err := testPool.QueryRow(context.Background(), `
		SELECT requester_id, target_unit_id, channel_type FROM dispatch_origin WHERE issue_id = $1
	`, res.Issue.ID).Scan(&requester, &targetUnit, &channel); err != nil {
		t.Fatalf("load dispatch origin: %v", err)
	}
	if requester != admin || targetUnit != unitID || channel != "app" {
		t.Fatalf("origin = %s/%s/%s, want %s/%s/app", requester, targetUnit, channel, admin, unitID)
	}

	// A second dispatch reuses the department squad instead of creating another.
	status, again, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title":       "dispatch-test 第二次派活",
		"description": "## 目标\n再来一次",
	})
	if status != http.StatusCreated {
		t.Fatalf("second dispatch status = %d, want 201: %s", status, body)
	}
	if again.Issue.AssigneeID == nil || *again.Issue.AssigneeID != squadID {
		t.Fatalf("second assignee id = %v, want squad %s", again.Issue.AssigneeID, squadID)
	}
}

func TestDispatchToOrgUnit_EnforcesVisibilityAndLeaderAccess(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-scope-admin@example.com")
	member := createPermissionTestMember(t, "dispatch-scope-member@example.com")
	memberAgent := insertIntakeDutyAgent(t, "dispatch-scope-member-agent", member)
	adminAgent := insertIntakeDutyAgent(t, "dispatch-scope-admin-agent", admin)

	ownUnit := createOrgUnitAs(t, admin, "成员所属部", "")
	hiddenUnit := createOrgUnitAs(t, admin, "其他部门", "")
	emptyUnit := createOrgUnitAs(t, admin, "无 Agent 部门", "")
	placeOrgMember(t, member, ownUnit)
	placeOrgAgent(t, admin, memberAgent, ownUnit)
	placeOrgAgent(t, admin, adminAgent, hiddenUnit)
	setOrgUnitLead(t, ownUnit, member)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	// The member can dispatch to their own department: its lead owns the leader
	// agent, so the invocation gate passes.
	status, _, body := dispatchToOrgUnit(t, member, ownUnit, map[string]any{
		"title":       "dispatch-test 成员派活",
		"description": "## 目标\n成员给自己部门派活",
	})
	if status != http.StatusCreated {
		t.Fatalf("member dispatch status = %d, want 201: %s", status, body)
	}

	// A department outside the member's visible subtree answers 404, not 403,
	// so members cannot enumerate the rest of the company.
	if status, _, body := dispatchToOrgUnit(t, member, hiddenUnit, map[string]any{
		"title": "dispatch-test 越权", "description": "x",
	}); status != http.StatusNotFound {
		t.Fatalf("hidden unit status = %d, want 404: %s", status, body)
	}

	// The member can see a department they were placed in, but its only agent
	// belongs to the admin and is not shared, so the dispatch is refused rather
	// than waking a private agent.
	leaderUnit := createOrgUnitAs(t, admin, "管理员私有人部门", "")
	placeOrgMember(t, member, leaderUnit)
	placeOrgAgent(t, admin, adminAgent, leaderUnit)
	if status, _, body := dispatchToOrgUnit(t, member, leaderUnit, map[string]any{
		"title": "dispatch-test 私有人", "description": "x",
	}); status != http.StatusForbidden {
		t.Fatalf("private leader status = %d, want 403: %s", status, body)
	}

	// A department with no agent cannot lead the work.
	if status, _, body := dispatchToOrgUnit(t, admin, emptyUnit, map[string]any{
		"title": "dispatch-test 空部门", "description": "x",
	}); status != http.StatusConflict {
		t.Fatalf("empty department status = %d, want 409: %s", status, body)
	}
}

func TestCompleteDispatch_NotifiesOncePerChannel(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-notify-admin@example.com")
	leader := insertIntakeDutyAgent(t, "dispatch-notify-leader", admin)
	unitID := createOrgUnitAs(t, admin, "回执部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	createDispatch := func(channel string, chatID string) db.Issue {
		result, err := testHandler.OrgDispatchService.Dispatch(context.Background(), service.OrgDispatchParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			ActorID:     parseUUID(admin),
			UnitID:      parseUUID(unitID),
			Title:       "dispatch-test 回执-" + channel,
			Description: "## 目标\n回执",
			Origin:      service.DispatchOriginInfo{ChannelType: channel, ChatID: chatID},
		})
		if err != nil {
			t.Fatalf("dispatch (%s): %v", channel, err)
		}
		return result.Issue
	}

	appIssue := createDispatch(service.DispatchChannelApp, "")
	if _, err := testHandler.OrgDispatchService.CompleteDispatch(context.Background(), appIssue); err != nil {
		t.Fatalf("complete app dispatch: %v", err)
	}
	count := func(issueID pgtype.UUID) int {
		var n int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = 'dispatch_completed'
		`, issueID).Scan(&n); err != nil {
			t.Fatalf("count inbox: %v", err)
		}
		return n
	}
	if n := count(appIssue.ID); n != 1 {
		t.Fatalf("app dispatch inbox items = %d, want 1", n)
	}
	// Duplicate status events must not notify twice.
	if _, err := testHandler.OrgDispatchService.CompleteDispatch(context.Background(), appIssue); err != nil {
		t.Fatalf("repeat completion: %v", err)
	}
	if n := count(appIssue.ID); n != 1 {
		t.Fatalf("repeat dispatch inbox items = %d, want 1", n)
	}

	// Chat dispatches return the origin for the transport to reply and do not
	// also write an inbox item.
	chatIssue := createDispatch(service.DispatchChannelFeishu, "oc_chat")
	notice, err := testHandler.OrgDispatchService.CompleteDispatch(context.Background(), chatIssue)
	if err != nil {
		t.Fatalf("complete chat dispatch: %v", err)
	}
	if notice == nil {
		t.Fatal("chat dispatch returned no notice")
	}
	if notice.Origin.ChatID.String != "oc_chat" || notice.UnitName != "回执部" {
		t.Fatalf("notice = %+v, want the originating chat and unit name", notice)
	}
	if n := count(chatIssue.ID); n != 0 {
		t.Fatalf("chat dispatch inbox items = %d, want 0", n)
	}
	if notice, _ := testHandler.OrgDispatchService.CompleteDispatch(context.Background(), chatIssue); notice != nil {
		t.Fatal("repeat chat completion returned a second notice")
	}
}

func TestIsDispatchCompleted(t *testing.T) {
	requireDB(t)
	wsUUID := parseUUID(testWorkspaceID)
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"todo", false},
		{"in_progress", false},
		{"blocked", false},
		{"in_review", true},
		{"done", true},
		{"cancelled", true},
		{"backlog", false},
	} {
		if got := service.IsDispatchCompleted(context.Background(), testHandler.Queries, wsUUID, tc.status); got != tc.want {
			t.Errorf("IsDispatchCompleted(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
