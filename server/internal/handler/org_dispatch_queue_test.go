package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listDepartmentDispatches(t *testing.T, userID string) (int, []dispatchQueueItem, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ListDepartmentDispatches(rec, newRequestAs(userID, "GET", "/api/org/dispatch-queue?limit=50", nil))
	var res struct {
		Dispatches []dispatchQueueItem `json:"dispatches"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode queue: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res.Dispatches, rec.Body.String()
}

func setIssueStatus(t *testing.T, issueID, status string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET status = $1 WHERE id = $2`, status, issueID); err != nil {
		t.Fatalf("set issue %s status to %s: %v", issueID, status, err)
	}
}

// TestListDepartmentDispatches_ScopedQueueWithStages covers the department
// workbench queue: dispatches are scoped to the units a caller leads, open
// work is split into waiting / in progress / review, finished work drops out,
// and an open blocker explains why an item is parked.
func TestListDepartmentDispatches_ScopedQueueWithStages(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "queue-admin@example.com")
	leadA := createPermissionTestMember(t, "queue-lead-a@example.com")
	leadB := createPermissionTestMember(t, "queue-lead-b@example.com")
	agentA := insertRunnableDutyAgent(t, "queue-agent-a", admin)
	agentB := insertRunnableDutyAgent(t, "queue-agent-b", admin)
	unitA := createOrgUnitAs(t, admin, "队列一部", "")
	unitB := createOrgUnitAs(t, admin, "队列二部", "")
	placeOrgAgent(t, admin, agentA, unitA)
	placeOrgAgent(t, admin, agentB, unitB)
	placeOrgMember(t, leadA, unitA)
	placeOrgMember(t, leadB, unitB)
	setOrgUnitLead(t, unitA, leadA)
	setOrgUnitLead(t, unitB, leadB)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	dispatch := func(unitID, title string) string {
		t.Helper()
		status, res, body := dispatchToOrgUnit(t, admin, unitID, map[string]any{
			"title": title, "description": "x",
		})
		if status != http.StatusCreated {
			t.Fatalf("dispatch %s status = %d, want 201: %s", title, status, body)
		}
		return res.Issue.ID
	}
	pendingIssue := dispatch(unitA, "dispatch-test 队列待承接")
	progressIssue := dispatch(unitA, "dispatch-test 队列进行中")
	reviewIssue := dispatch(unitA, "dispatch-test 队列待验收")
	doneIssue := dispatch(unitA, "dispatch-test 队列已完成")
	otherIssue := dispatch(unitB, "dispatch-test 二部队列")
	setIssueStatus(t, pendingIssue, "todo")
	setIssueStatus(t, progressIssue, "in_progress")
	setIssueStatus(t, reviewIssue, "in_review")
	setIssueStatus(t, doneIssue, "done")
	setIssueStatus(t, otherIssue, "in_progress")

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO intake_blocker (workspace_id, issue_id, done_text, missing_text, consequence_text, state)
		VALUES ($1, $2, '已完成一半', '需要数据库权限', '无法继续验收', 'open')
	`, testWorkspaceID, progressIssue); err != nil {
		t.Fatalf("insert blocker: %v", err)
	}

	status, items, body := listDepartmentDispatches(t, leadA)
	if status != http.StatusOK {
		t.Fatalf("queue status = %d, want 200: %s", status, body)
	}
	if len(items) != 3 {
		t.Fatalf("queue items = %d, want 3 (done work drops out, other units are out of scope): %s", len(items), body)
	}
	stages := make(map[string]string, len(items))
	blockers := make(map[string]*dispatchBlocker, len(items))
	for _, item := range items {
		stages[item.Issue.ID] = item.Stage
		blockers[item.Issue.ID] = item.Blocker
		if item.UnitID != unitA {
			t.Errorf("item %s unit = %s, want %s", item.Issue.ID, item.UnitID, unitA)
		}
		if item.RequesterName != "queue-admin@example.com" {
			t.Errorf("item %s requester = %q, want the dispatching user", item.Issue.ID, item.RequesterName)
		}
	}
	if got := stages[pendingIssue]; got != "pending" {
		t.Errorf("todo dispatch stage = %q, want pending", got)
	}
	if got := stages[progressIssue]; got != "in_progress" {
		t.Errorf("in_progress dispatch stage = %q, want in_progress", got)
	}
	if got := stages[reviewIssue]; got != "review" {
		t.Errorf("in_review dispatch stage = %q, want review", got)
	}
	blocker := blockers[progressIssue]
	if blocker == nil || blocker.Missing != "需要数据库权限" || blocker.Consequence != "无法继续验收" {
		t.Errorf("blocker = %+v, want the open intake blocker", blocker)
	}
	if blockers[pendingIssue] != nil {
		t.Errorf("pending dispatch carries a blocker it does not have")
	}

	status, items, body = listDepartmentDispatches(t, leadB)
	if status != http.StatusOK {
		t.Fatalf("second lead queue status = %d, want 200: %s", status, body)
	}
	if len(items) != 1 || items[0].Issue.ID != otherIssue {
		t.Fatalf("second lead queue = %+v, want only their own unit's dispatch", items)
	}

	// A workspace manager sees every department's queue.
	status, items, body = listDepartmentDispatches(t, admin)
	if status != http.StatusOK {
		t.Fatalf("manager queue status = %d, want 200: %s", status, body)
	}
	if len(items) != 4 {
		t.Fatalf("manager queue items = %d, want 4: %s", len(items), body)
	}
}
