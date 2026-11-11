package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIntakeDecision_RejectsTaskFromAnotherIssue(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-foreign-lead@example.com")
	owner := createPermissionTestMember(t, "intake-foreign-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-foreign-agent", owner)
	issueID := insertIntakeIssue(t, projectID, agentID, lead)
	otherID := insertIntakeIssue(t, projectID, agentID, lead)
	taskID := insertIntakeTask(t, agentID, otherID, "running")
	rec := httptest.NewRecorder()
	testHandler.CreateIntakeBlocker(rec, intakeAgentRequest(t, issueID, "id", lead, agentID, taskID, map[string]any{
		"task_id": taskID, "done": "做了", "missing": "缺", "consequence": "会错",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIntakeDecision_ExhaustedContinueAllowsEmptyBody(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-exhausted-lead@example.com")
	owner := createPermissionTestMember(t, "intake-exhausted-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-exhausted-agent", owner)
	issueID := insertIntakeIssue(t, projectID, agentID, lead)
	moduleID := insertPlainModule(t, projectID, agentID, "")
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "没有进展",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE intake_request SET progress_done = '还没做完', progress_remain = '执行没有进展' WHERE issue_id = $1`, issueID); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	testHandler.ContinueIntakeRequest(rec, intakeDecisionRequest(t, "POST", "/continue", issueID, "issueId", lead, map[string]any{}))
	if rec.Code != http.StatusOK || !containsAll(rec.Body.String(), "continued") {
		t.Fatalf("continue = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIntakeDecision_BlockerContinueStopAcceptCancelResume(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-decide-lead@example.com")
	owner := createPermissionTestMember(t, "intake-decide-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-decide-agent", owner)
	backupID := insertIntakeDutyAgent(t, "intake-decide-backup", owner)
	issueID := insertIntakeIssue(t, projectID, agentID, lead)
	moduleID := insertIntakeModuleRow(t, projectID, agentID, backupID)
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "把导出接上",
	}); err != nil {
		t.Fatal(err)
	}
	taskID := insertIntakeTask(t, agentID, issueID, "queued")

	blank := intakeDecisionRequest(t, "POST", "/api/issues/"+issueID+"/intake-blocker", issueID, "id", lead, map[string]any{
		"task_id": taskID, "done": " ", "missing": "格式", "consequence": "可能导错",
	})
	blank.Header.Set("X-Agent-ID", agentID)
	blank.Header.Set("X-Task-ID", taskID)
	blankRec := httptest.NewRecorder()
	testHandler.CreateIntakeBlocker(blankRec, blank)
	if blankRec.Code != http.StatusBadRequest {
		t.Fatalf("blank blocker = %d: %s", blankRec.Code, blankRec.Body.String())
	}

	blockerBody := map[string]any{
		"task_id": taskID, "done": "列表出来了", "missing": "导出格式", "consequence": "按表格可能不对",
	}
	openRec := httptest.NewRecorder()
	testHandler.CreateIntakeBlocker(openRec, intakeAgentRequest(t, issueID, "id", lead, agentID, taskID, blockerBody))
	if openRec.Code != http.StatusCreated || !containsAll(openRec.Body.String(), "open") {
		t.Fatalf("open blocker = %d: %s", openRec.Code, openRec.Body.String())
	}
	if status := intakeIssueStatus(t, issueID); status != "blocked" {
		t.Fatalf("status after blocker = %s", status)
	}

	againRec := httptest.NewRecorder()
	testHandler.CreateIntakeBlocker(againRec, intakeAgentRequest(t, issueID, "id", lead, agentID, taskID, blockerBody))
	if againRec.Code != http.StatusConflict {
		t.Fatalf("second blocker = %d: %s", againRec.Code, againRec.Body.String())
	}

	emptyContinue := intakeDecisionRequest(t, "POST", "/continue", issueID, "issueId", lead, map[string]any{})
	emptyRec := httptest.NewRecorder()
	testHandler.ContinueIntakeRequest(emptyRec, emptyContinue)
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("empty continue = %d: %s", emptyRec.Code, emptyRec.Body.String())
	}

	testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, taskID)
	cont := intakeDecisionRequest(t, "POST", "/continue", issueID, "issueId", lead, map[string]any{"body": "用 csv"})
	contRec := httptest.NewRecorder()
	testHandler.ContinueIntakeRequest(contRec, cont)
	if contRec.Code != http.StatusOK || !containsAll(contRec.Body.String(), "continued") {
		t.Fatalf("continue = %d: %s", contRec.Code, contRec.Body.String())
	}

	stopIssue := insertIntakeIssue(t, projectID, agentID, lead)
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(stopIssue), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "停掉这条",
	}); err != nil {
		t.Fatal(err)
	}
	stopTask := insertIntakeTask(t, agentID, stopIssue, "running")
	stopOpenRec := httptest.NewRecorder()
	testHandler.CreateIntakeBlocker(stopOpenRec, intakeAgentRequest(t, stopIssue, "id", lead, agentID, stopTask, map[string]any{
		"task_id": stopTask, "done": "做到一半", "missing": "口径", "consequence": "数会错",
	}))
	if stopOpenRec.Code != http.StatusCreated {
		t.Fatalf("stop blocker = %d: %s", stopOpenRec.Code, stopOpenRec.Body.String())
	}
	stopReq := intakeDecisionRequest(t, "POST", "/stop", stopIssue, "issueId", lead, map[string]any{})
	stopRec := httptest.NewRecorder()
	testHandler.StopIntakeRequest(stopRec, stopReq)
	if stopRec.Code != http.StatusOK || intakeIssueStatus(t, stopIssue) != "blocked" {
		t.Fatalf("stop = %d status %s: %s", stopRec.Code, intakeIssueStatus(t, stopIssue), stopRec.Body.String())
	}
	var remain string
	if err := testPool.QueryRow(context.Background(), `SELECT progress_remain FROM intake_request WHERE issue_id = $1`, stopIssue).Scan(&remain); err != nil {
		t.Fatal(err)
	}
	if remain != "提出人要求停止" {
		t.Fatalf("remain = %q", remain)
	}

	review := insertIntakeIssue(t, projectID, agentID, lead)
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(review), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "等验收",
	}); err != nil {
		t.Fatal(err)
	}
	early := intakeDecisionRequest(t, "POST", "/accept", review, "issueId", lead, nil)
	earlyRec := httptest.NewRecorder()
	testHandler.AcceptIntakeRequest(earlyRec, early)
	if earlyRec.Code != http.StatusConflict {
		t.Fatalf("early accept = %d: %s", earlyRec.Code, earlyRec.Body.String())
	}
	testPool.Exec(context.Background(), `UPDATE issue SET status = 'in_review' WHERE id = $1`, review)
	accept := intakeDecisionRequest(t, "POST", "/accept", review, "issueId", lead, nil)
	acceptRec := httptest.NewRecorder()
	testHandler.AcceptIntakeRequest(acceptRec, accept)
	if acceptRec.Code != http.StatusOK || intakeIssueStatus(t, review) != "done" {
		t.Fatalf("accept = %d status %s: %s", acceptRec.Code, intakeIssueStatus(t, review), acceptRec.Body.String())
	}

	cancelIssue := insertIntakeIssue(t, projectID, agentID, lead)
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(cancelIssue), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "还没开始",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO issue_wakeup (id, workspace_id, issue_id, agent_id, created_by, instruction, kind, mode, timezone)
		VALUES ($1, $2, $3, $4, $5, 'later', 'at', 'once', 'UTC')
	`, uuid.NewString(), testWorkspaceID, cancelIssue, agentID, lead); err != nil {
		t.Fatal(err)
	}
	cancel := intakeDecisionRequest(t, "POST", "/cancel", cancelIssue, "issueId", lead, nil)
	cancelRec := httptest.NewRecorder()
	testHandler.CancelIntakeRequest(cancelRec, cancel)
	if cancelRec.Code != http.StatusOK || intakeIssueStatus(t, cancelIssue) != "cancelled" {
		t.Fatalf("cancel = %d status %s: %s", cancelRec.Code, intakeIssueStatus(t, cancelIssue), cancelRec.Body.String())
	}
	var enabled bool
	if err := testPool.QueryRow(context.Background(), `SELECT enabled FROM issue_wakeup WHERE issue_id = $1`, cancelIssue).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("wakeup still enabled")
	}

	resume := intakeDecisionRequest(t, "POST", "/resume", issueID, "issueId", lead, nil)
	resumeRec := httptest.NewRecorder()
	testHandler.ResumeIntakeRequest(resumeRec, resume)
	if resumeRec.Code != http.StatusOK || !containsAll(resumeRec.Body.String(), "waiting") {
		t.Fatalf("resume = %d: %s", resumeRec.Code, resumeRec.Body.String())
	}
	var progress string
	if err := testPool.QueryRow(context.Background(), `SELECT progress_remain FROM intake_request WHERE issue_id = $1`, issueID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if progress != "需要维护" {
		t.Fatalf("resume remain = %q", progress)
	}
}

func intakeAgentRequest(t *testing.T, issueID, param, userID, agentID, taskID string, body any) *http.Request {
	t.Helper()
	req := intakeDecisionRequest(t, "POST", "/blocker", issueID, param, userID, body)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func intakeDecisionRequest(t *testing.T, method, path, issueID, param, userID string, body any) *http.Request {
	t.Helper()
	req := withURLParam(newRequest(method, path, body), param, issueID)
	req.Header.Set("X-User-ID", userID)
	return req
}

func insertIntakeIssue(t *testing.T, projectID, agentID, creatorID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number, position)
		VALUES ($1, $2, 'intake decision', 'todo', 'none', 'member', $3, 'agent', $4,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id
	`, testWorkspaceID, projectID, creatorID, agentID).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return id
}

func insertIntakeModuleRow(t *testing.T, projectID, agentID, backupID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO intake_module (workspace_id, project_id, product, module, scope, agent_id, backup_agent_id, position)
		VALUES ($1, $2, '结算', '决策', '只测决策', $3, $4, 0) RETURNING id
	`, testWorkspaceID, projectID, agentID, backupID).Scan(&id); err != nil {
		t.Fatalf("insert module: %v", err)
	}
	return id
}

func insertIntakeTask(t *testing.T, agentID, issueID, status string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, $4, 0) RETURNING id
	`, agentID, handlerTestRuntimeID(t), issueID, status).Scan(&id); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id)
	})
	return id
}

func intakeIssueStatus(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}
