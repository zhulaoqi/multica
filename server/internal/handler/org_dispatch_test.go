package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// insertRunnableDutyAgent creates a department agent that can lead work right
// now: dispatch checks the squad leader's runtime before creating the issue, so
// an agent with no runtime would be refused instead of dispatched to.
func insertRunnableDutyAgent(t *testing.T, name, ownerID string) string {
	t.Helper()
	agentID := insertIntakeDutyAgent(t, name, ownerID)
	makeAgentRunnable(t, ownerID, agentID)
	return agentID
}

// makeAgentRunnable binds an online runtime owned by the agent's owner. The
// runtime row is cleaned up with the test, mirroring newTestRuntime.
func makeAgentRunnable(t *testing.T, ownerID, agentID string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'test', 'online', 'dispatch test runtime', '{}'::jsonb, $3, now())
		RETURNING id
	`, testWorkspaceID, "dispatch-runtime-"+agentID, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime for agent %s: %v", agentID, err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET runtime_id = $1 WHERE id = $2`, runtimeID, agentID); err != nil {
		t.Fatalf("bind runtime to agent %s: %v", agentID, err)
	}
	t.Cleanup(func() {
		// The agent still points at the runtime when cleanups run in reverse
		// registration order, so unbind before deleting the row.
		testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = NULL WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
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
	testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type IN ('dispatch_completed', 'dispatch_failed')`, testWorkspaceID)
	// The leader tasks pin both the issue and the agent, so they have to go
	// before the agent cleanups can delete the fixture agents.
	testPool.Exec(ctx, `
		DELETE FROM agent_task_queue WHERE issue_id IN (
			SELECT id FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch-test-%'
		)
	`, testWorkspaceID)
	// Blockers are created without a foreign key, so deleting the issues above
	// would otherwise leave rows pointing at nothing.
	testPool.Exec(ctx, `
		DELETE FROM intake_blocker WHERE issue_id IN (
			SELECT id FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch-test-%'
		)
	`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch-test-%'`, testWorkspaceID)
	testPool.Exec(ctx, `
		DELETE FROM squad_member WHERE squad_id IN (
			SELECT id FROM squad WHERE workspace_id = $1 AND (org_unit_id IS NOT NULL OR is_private)
		)
	`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM squad WHERE workspace_id = $1 AND (org_unit_id IS NOT NULL OR is_private)`, testWorkspaceID)
}

func TestDispatchToOrgUnit_CreatesSquadIssueAndOrigin(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-admin@example.com")
	leader := insertRunnableDutyAgent(t, "dispatch-leader", admin)
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

// TestDispatchToOrgUnit_MovesSquadToOnlineLeader covers the failover a dispatch
// performs when the squad's leader machine went away: another online agent in
// the department takes over instead of the issue sitting behind a dead runtime.
func TestDispatchToOrgUnit_MovesSquadToOnlineLeader(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-failover-admin@example.com")
	leader := insertIntakeDutyAgent(t, "dispatch-failover-a-leader", admin)
	leaderRuntime := makeAgentRunnable(t, admin, leader)
	unitID := createOrgUnitAs(t, admin, "故障转移部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	if status, _, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title": "dispatch-test 首次派活", "description": "x",
	}); status != http.StatusCreated {
		t.Fatalf("first dispatch status = %d, want 201: %s", status, body)
	}

	backup := insertRunnableDutyAgent(t, "dispatch-failover-b-backup", admin)
	placeOrgAgent(t, admin, backup, unitID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, leaderRuntime); err != nil {
		t.Fatalf("take leader runtime offline: %v", err)
	}

	status, _, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title": "dispatch-test 兜底派活", "description": "x",
	})
	if status != http.StatusCreated {
		t.Fatalf("failover dispatch status = %d, want 201: %s", status, body)
	}
	var squadLeader string
	if err := testPool.QueryRow(context.Background(), `
		SELECT leader_id FROM squad WHERE workspace_id = $1 AND org_unit_id = $2
	`, testWorkspaceID, unitID).Scan(&squadLeader); err != nil {
		t.Fatalf("load department squad: %v", err)
	}
	if squadLeader != backup {
		t.Fatalf("squad leader = %s, want the online backup %s", squadLeader, backup)
	}
	var backupRole string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role FROM squad_member WHERE squad_id = (
			SELECT id FROM squad WHERE workspace_id = $1 AND org_unit_id = $2
		) AND member_id = $3 AND member_type = 'agent'
	`, testWorkspaceID, unitID, backup).Scan(&backupRole); err != nil {
		t.Fatalf("load new leader squad member: %v", err)
	}
	if backupRole != "leader" {
		t.Fatalf("backup squad role = %q, want leader", backupRole)
	}
}

// TestDispatchToOrgUnit_RefusesWhenEveryAgentOffline returns a specific error
// code instead of creating an issue no leader can pick up.
func TestDispatchToOrgUnit_RefusesWhenEveryAgentOffline(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-offline-admin@example.com")
	agent := insertIntakeDutyAgent(t, "dispatch-offline-agent", admin)
	unitID := createOrgUnitAs(t, admin, "全员离线部", "")
	placeOrgAgent(t, admin, agent, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, _, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title": "dispatch-test 离线", "description": "x",
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", status, body)
	}
	if !strings.Contains(body, "org_dispatch_leader_offline") {
		t.Fatalf("body = %s, want org_dispatch_leader_offline", body)
	}
}

func TestDispatchToOrgUnit_EnforcesVisibilityAndLeaderAccess(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-scope-admin@example.com")
	member := createPermissionTestMember(t, "dispatch-scope-member@example.com")
	memberAgent := insertRunnableDutyAgent(t, "dispatch-scope-member-agent", member)
	adminAgent := insertRunnableDutyAgent(t, "dispatch-scope-admin-agent", admin)

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
	leader := insertRunnableDutyAgent(t, "dispatch-notify-leader", admin)
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

// TestFailDispatch_NotifiesOnceForLeaderTask covers the requester's failure
// notice: only the squad-leader run is a dispatch failure, and a repeated
// failure event does not notify twice.
func TestFailDispatch_NotifiesOnceForLeaderTask(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-fail-admin@example.com")
	leader := insertRunnableDutyAgent(t, "dispatch-fail-leader", admin)
	unitID := createOrgUnitAs(t, admin, "失败通知部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
		"title": "dispatch-test 失败通知", "description": "x",
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(res.Issue.ID))
	if err != nil {
		t.Fatalf("load dispatched issue: %v", err)
	}
	var leaderTask string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_task_queue WHERE issue_id = $1 AND is_leader_task
		ORDER BY created_at DESC LIMIT 1
	`, issue.ID).Scan(&leaderTask); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	count := func() int {
		var n int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = 'dispatch_failed'
		`, issue.ID).Scan(&n); err != nil {
			t.Fatalf("count inbox: %v", err)
		}
		return n
	}

	notice, err := testHandler.OrgDispatchService.FailDispatch(context.Background(), issue, parseUUID(leaderTask), "agent timeout")
	if err != nil {
		t.Fatalf("fail dispatch: %v", err)
	}
	if notice != nil {
		t.Fatalf("app dispatch returned a chat notice: %+v", notice)
	}
	if n := count(); n != 1 {
		t.Fatalf("failure inbox items = %d, want 1", n)
	}

	// A repeated failure event finds the claim already taken.
	if _, err := testHandler.OrgDispatchService.FailDispatch(context.Background(), issue, parseUUID(leaderTask), "agent timeout"); err != nil {
		t.Fatalf("repeat failure: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("repeat failure inbox items = %d, want 1", n)
	}

	// A child task failing is ordinary work, not the department giving up.
	var childTask string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, squad_id, status, is_leader_task, runtime_id)
		SELECT agent_id, issue_id, squad_id, 'failed', false, runtime_id
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, leaderTask).Scan(&childTask); err != nil {
		t.Fatalf("insert child task: %v", err)
	}
	if _, err := testHandler.OrgDispatchService.FailDispatch(context.Background(), issue, parseUUID(childTask), "x"); err != nil {
		t.Fatalf("fail child task: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("child failure inbox items = %d, want 1", n)
	}
}

func TestFailDispatch_ChatOriginReturnsNoticeWithoutInbox(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "dispatch-fail-chat-admin@example.com")
	leader := insertRunnableDutyAgent(t, "dispatch-fail-chat-leader", admin)
	unitID := createOrgUnitAs(t, admin, "失败回执部", "")
	placeOrgAgent(t, admin, leader, unitID)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	result, err := testHandler.OrgDispatchService.Dispatch(context.Background(), service.OrgDispatchParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		ActorID:     parseUUID(admin),
		UnitID:      parseUUID(unitID),
		Title:       "dispatch-test 聊天失败",
		Description: "x",
		Origin:      service.DispatchOriginInfo{ChannelType: service.DispatchChannelFeishu, ChatID: "oc_chat"},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var leaderTask string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_task_queue WHERE issue_id = $1 AND is_leader_task
		ORDER BY created_at DESC LIMIT 1
	`, result.Issue.ID).Scan(&leaderTask); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	notice, err := testHandler.OrgDispatchService.FailDispatch(context.Background(), result.Issue, parseUUID(leaderTask), "boom")
	if err != nil {
		t.Fatalf("fail dispatch: %v", err)
	}
	if notice == nil || notice.Origin.ChatID.String != "oc_chat" || notice.UnitName != "失败回执部" {
		t.Fatalf("notice = %+v, want the originating chat and unit", notice)
	}
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = 'dispatch_failed'
	`, result.Issue.ID).Scan(&n); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if n != 0 {
		t.Fatalf("chat failure inbox items = %d, want 0", n)
	}
}

func reassignDispatch(t *testing.T, userID, issueID, unitID string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ReassignDispatch(rec, orgRequestWithParam(userID, "POST", "/api/org/dispatches/x/reassign", "issueId", issueID, map[string]any{
		"unit_id": unitID,
	}))
	return rec.Code, rec.Body.String()
}

// TestReassignDispatch_MovesWorkAndWakesNewLeader covers the requester moving
// their own dispatch: the issue, the origin and the leader task all follow the
// new department, and the previous notices are cleared for the new delivery.
func TestReassignDispatch_MovesWorkAndWakesNewLeader(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "reassign-admin@example.com")
	leaderA := insertRunnableDutyAgent(t, "reassign-leader-a", admin)
	leaderB := insertRunnableDutyAgent(t, "reassign-leader-b", admin)
	unitA := createOrgUnitAs(t, admin, "改派甲部", "")
	unitB := createOrgUnitAs(t, admin, "改派乙部", "")
	placeOrgAgent(t, admin, leaderA, unitA)
	placeOrgAgent(t, admin, leaderB, unitB)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := dispatchToOrgUnit(t, admin, unitA, map[string]any{
		"title": "dispatch-test 改派", "description": "x",
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}
	// Pretend the first delivery already reported something; a reassignment
	// must clear both claims.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE dispatch_origin SET notified_at = now(), failure_notified_at = now() WHERE issue_id = $1
	`, parseUUID(res.Issue.ID)); err != nil {
		t.Fatalf("seed dispatch notices: %v", err)
	}

	status, body = reassignDispatch(t, admin, res.Issue.ID, unitB)
	if status != http.StatusOK {
		t.Fatalf("reassign status = %d, want 200: %s", status, body)
	}
	var squadB, assignee string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM squad WHERE workspace_id = $1 AND org_unit_id = $2
	`, testWorkspaceID, unitB).Scan(&squadB); err != nil {
		t.Fatalf("load target squad: %v", err)
	}
	var assigneeType string
	if err := testPool.QueryRow(context.Background(), `
		SELECT assignee_type, assignee_id FROM issue WHERE id = $1
	`, parseUUID(res.Issue.ID)).Scan(&assigneeType, &assignee); err != nil {
		t.Fatalf("load issue assignee: %v", err)
	}
	if assigneeType != "squad" || assignee != squadB {
		t.Fatalf("assignee = %s/%s, want squad/%s", assigneeType, assignee, squadB)
	}
	var targetUnit string
	var notified, failed bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT target_unit_id, notified_at IS NOT NULL, failure_notified_at IS NOT NULL
		FROM dispatch_origin WHERE issue_id = $1
	`, parseUUID(res.Issue.ID)).Scan(&targetUnit, &notified, &failed); err != nil {
		t.Fatalf("load dispatch origin: %v", err)
	}
	if targetUnit != unitB {
		t.Fatalf("origin target unit = %s, want %s", targetUnit, unitB)
	}
	if notified || failed {
		t.Fatalf("notice claims = notified:%v failed:%v, want both cleared", notified, failed)
	}
	var queued int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND is_leader_task AND squad_id = $2
	`, parseUUID(res.Issue.ID), parseUUID(squadB)).Scan(&queued); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if queued != 1 {
		t.Fatalf("leader tasks for the new squad = %d, want 1", queued)
	}
}

func TestReassignDispatch_RejectsOtherMembersAndOfflineDepartment(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "reassign-scope-admin@example.com")
	other := createPermissionTestMember(t, "reassign-scope-other@example.com")
	leader := insertRunnableDutyAgent(t, "reassign-scope-leader", admin)
	unit := createOrgUnitAs(t, admin, "改派权限部", "")
	offlineUnit := createOrgUnitAs(t, admin, "改派离线部", "")
	placeOrgAgent(t, admin, leader, unit)
	offlineAgent := insertIntakeDutyAgent(t, "reassign-scope-offline", admin)
	placeOrgAgent(t, admin, offlineAgent, offlineUnit)
	placeOrgMember(t, other, unit)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := dispatchToOrgUnit(t, admin, unit, map[string]any{
		"title": "dispatch-test 改派权限", "description": "x",
	})
	if status != http.StatusCreated {
		t.Fatalf("dispatch status = %d, want 201: %s", status, body)
	}

	if status, body := reassignDispatch(t, other, res.Issue.ID, unit); status != http.StatusForbidden {
		t.Fatalf("other member reassign status = %d, want 403: %s", status, body)
	}
	if status, body := reassignDispatch(t, admin, res.Issue.ID, offlineUnit); status != http.StatusConflict ||
		!strings.Contains(body, "org_dispatch_leader_offline") {
		t.Fatalf("offline department reassign = %d %s, want 409 org_dispatch_leader_offline", status, body)
	}
	if status, body := reassignDispatch(t, admin, res.Issue.ID, uuidToString(pgtype.UUID{})); status != http.StatusBadRequest {
		t.Fatalf("invalid unit status = %d, want 400: %s", status, body)
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
