package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

type transferSetup struct {
	projectDispatchSetup
	leaderID string
	issueID  string
	taskID   string
}

// setupDispatchTransfer routes a request to the department that owns the
// project, so the department squad leader holds a dispatch it may hand over.
func setupDispatchTransfer(t *testing.T, prefix, projectName string) transferSetup {
	t.Helper()
	setup := setupProjectDispatch(t, prefix, projectName, true)
	leader := insertRunnableDutyAgent(t, prefix+"-leader", setup.admin)
	placeOrgAgent(t, setup.admin, leader, setup.unitID)
	addOrgIntakeSource(t, setup.unitID, "user", setup.requester)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`, setup.projectID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE workspace_id = $1`, setup.projectID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, setup.projectID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE workspace_id = $1 AND issue_id IN (
			SELECT id FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch-test-%'
		)`, testWorkspaceID)
	})

	status, res, body := dispatchToOrgUnit(t, setup.requester, setup.unitID, map[string]any{
		"title":       "dispatch-test " + projectName + " 接了哪些模型",
		"description": "列出当前接入的模型",
	})
	if status != http.StatusCreated {
		t.Fatalf("department dispatch status = %d, want 201: %s", status, body)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND is_leader_task
	`, res.Issue.ID, leader).Scan(&taskID); err != nil {
		t.Fatalf("load department leader task: %v", err)
	}
	return transferSetup{projectDispatchSetup: setup, leaderID: leader, issueID: res.Issue.ID, taskID: taskID}
}

func transferDispatch(t *testing.T, issueID, agentID, taskID string, body map[string]any) (int, transferDispatchResponse, string) {
	t.Helper()
	req := agentRequest(http.MethodPost, "/api/org/dispatches/"+issueID+"/transfer-to-project", body, agentID, taskID)
	rec := httptest.NewRecorder()
	testHandler.TransferDispatchToProject(rec, withURLParam(req, "issueId", issueID))
	var res transferDispatchResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode transfer response: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res, rec.Body.String()
}

func TestTransferDispatchToProject_MovesTheRequestIntoTheProject(t *testing.T) {
	setup := setupDispatchTransfer(t, "transfer-move", "AIHUB转交")

	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, issue_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, $2, 'member', $3, 'models.xlsx', 'https://files.example/models.xlsx', 'application/octet-stream', 512)
		RETURNING id::text
	`, testWorkspaceID, setup.issueID, setup.requester).Scan(&attachmentID); err != nil {
		t.Fatalf("seed dispatch attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})

	status, res, body := transferDispatch(t, setup.issueID, setup.leaderID, setup.taskID, map[string]any{
		"project": setup.projectID,
		"note":    "问的是 AIHUB 的模型",
	})
	if status != http.StatusOK {
		t.Fatalf("transfer status = %d, want 200: %s", status, body)
	}
	if res.Issue.WorkspaceID != setup.projectID {
		t.Fatalf("new issue workspace = %s, want project %s", res.Issue.WorkspaceID, setup.projectID)
	}
	if res.Issue.AssigneeType == nil || *res.Issue.AssigneeType != "agent" ||
		res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != setup.agentID {
		t.Fatalf("new issue assignee = %v/%v, want project agent %s", res.Issue.AssigneeType, res.Issue.AssigneeID, setup.agentID)
	}
	if res.ExecutorID != setup.agentID || res.Project.ID != setup.projectID {
		t.Fatalf("response executor/project = %s/%s, want %s/%s", res.ExecutorID, res.Project.ID, setup.agentID, setup.projectID)
	}
	if res.Previous.ID != setup.issueID || res.Previous.Status != "cancelled" {
		t.Fatalf("previous = %s/%s, want %s cancelled", res.Previous.ID, res.Previous.Status, setup.issueID)
	}

	var originIssue, targetProject string
	var targetSquad pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		SELECT issue_id, target_project_id, target_squad_id FROM dispatch_origin WHERE requester_id = $1
	`, setup.requester).Scan(&originIssue, &targetProject, &targetSquad); err != nil {
		t.Fatalf("load dispatch origin: %v", err)
	}
	if originIssue != res.Issue.ID || targetProject != setup.projectID || targetSquad.Valid {
		t.Fatalf("origin = %s/%s/%v, want it to follow %s in %s", originIssue, targetProject, targetSquad, res.Issue.ID, setup.projectID)
	}

	var leaderTask bool
	var squadID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		SELECT is_leader_task, squad_id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2
	`, res.Issue.ID, setup.agentID).Scan(&leaderTask, &squadID); err != nil {
		t.Fatalf("load project executor task: %v", err)
	}
	if !leaderTask || squadID.Valid {
		t.Fatalf("executor task leader/squad = %v/%v, want a leader task without a squad", leaderTask, squadID)
	}

	// The caller's own run finishes its turn; only the other runs go.
	var leaderStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, setup.taskID).Scan(&leaderStatus); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	if leaderStatus == "cancelled" {
		t.Fatalf("leader task was cancelled, want it left to finish its turn")
	}

	var attachmentWorkspace, attachmentIssue string
	if err := testPool.QueryRow(context.Background(), `
		SELECT workspace_id, issue_id FROM attachment WHERE id = $1
	`, attachmentID).Scan(&attachmentWorkspace, &attachmentIssue); err != nil {
		t.Fatalf("load moved attachment: %v", err)
	}
	if attachmentWorkspace != setup.projectID || attachmentIssue != res.Issue.ID {
		t.Fatalf("attachment = %s/%s, want project issue %s", attachmentWorkspace, attachmentIssue, res.Issue.ID)
	}

	var handoff string
	if err := testPool.QueryRow(context.Background(), `
		SELECT content FROM comment WHERE issue_id = $1 AND author_id = $2 AND type = 'system'
	`, setup.issueID, setup.leaderID).Scan(&handoff); err != nil {
		t.Fatalf("load handoff comment: %v", err)
	}
	if !strings.Contains(handoff, "AIHUB转交") || !strings.Contains(handoff, res.Issue.Identifier) || !strings.Contains(handoff, "问的是 AIHUB 的模型") {
		t.Fatalf("handoff comment = %q, want project, identifier and note", handoff)
	}

	// The origin already follows the project issue, so a second hop is refused.
	status, _, body = transferDispatch(t, setup.issueID, setup.leaderID, setup.taskID, map[string]any{
		"project": setup.projectID,
	})
	if status != http.StatusNotFound && status != http.StatusConflict {
		t.Fatalf("repeated transfer status = %d, want 404 or 409: %s", status, body)
	}
	if n := countRows(t, `SELECT count(*) FROM issue WHERE workspace_id = $1`, setup.projectID); n != 1 {
		t.Fatalf("project holds %d issues, want exactly 1", n)
	}
}

func TestTransferDispatchToProject_OnlyTheDepartmentLeaderRunMayTransfer(t *testing.T) {
	setup := setupDispatchTransfer(t, "transfer-auth", "转交鉴权")

	// A member request carries no task token.
	req := orgRequestWithParam(setup.admin, http.MethodPost, "/api/org/dispatches/x/transfer-to-project", "issueId", setup.issueID,
		map[string]any{"project": setup.projectID})
	rec := httptest.NewRecorder()
	testHandler.TransferDispatchToProject(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member transfer status = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	// Another agent is not the squad leader, even with the leader's task id.
	other := insertRunnableDutyAgent(t, "transfer-auth-other", setup.admin)
	status, _, body := transferDispatch(t, setup.issueID, other, setup.taskID, map[string]any{"project": setup.projectID})
	if status != http.StatusForbidden || !strings.Contains(body, "dispatch_transfer_forbidden") {
		t.Fatalf("non-leader transfer = %d %s, want 403 dispatch_transfer_forbidden", status, body)
	}

	// The leader must use a run of its own on this issue.
	status, _, body = transferDispatch(t, setup.issueID, setup.leaderID, "0193f1a2-0000-7000-8000-00000000dead", map[string]any{"project": setup.projectID})
	if status != http.StatusForbidden {
		t.Fatalf("unknown task transfer = %d %s, want 403", status, body)
	}

	// An agent outside the project cannot be named to run it.
	status, _, body = transferDispatch(t, setup.issueID, setup.leaderID, setup.taskID, map[string]any{
		"project":  setup.projectID,
		"agent_id": other,
	})
	if status != http.StatusConflict || !strings.Contains(body, "dispatch_transfer_agent_unavailable") {
		t.Fatalf("foreign agent transfer = %d %s, want 409 dispatch_transfer_agent_unavailable", status, body)
	}

	// An unknown project slug answers like a project the requester may not use.
	status, _, body = transferDispatch(t, setup.issueID, setup.leaderID, setup.taskID, map[string]any{"project": "no-such-project"})
	if status != http.StatusNotFound || !strings.Contains(body, "org_dispatch_unit_not_found") {
		t.Fatalf("unknown project transfer = %d %s, want 404 org_dispatch_unit_not_found", status, body)
	}

	if n := countRows(t, `SELECT count(*) FROM issue WHERE workspace_id = $1`, setup.projectID); n != 0 {
		t.Fatalf("refused transfers created %d project issues, want 0", n)
	}
	var issueStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, setup.issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("load department issue: %v", err)
	}
	if issueStatus == "cancelled" {
		t.Fatalf("refused transfers closed the department issue")
	}
}

func TestDispatchTransferBriefing_ListsRequestableProjects(t *testing.T) {
	setup := setupDispatchTransfer(t, "transfer-brief", "简报项目")

	briefing := testHandler.OrgDispatchService.DispatchTransferBriefing(context.Background(), parseUUID(setup.issueID))
	for _, want := range []string{"## Project Handoff", "multica squad transfer-to-project " + setup.issueID, "简报项目", setup.projectID, setup.agentID} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("briefing missing %q:\n%s", want, briefing)
		}
	}

	// Once transferred, the issue is no longer the department's to hand over.
	if status, _, body := transferDispatch(t, setup.issueID, setup.leaderID, setup.taskID, map[string]any{"project": setup.projectID}); status != http.StatusOK {
		t.Fatalf("transfer status = %d, want 200: %s", status, body)
	}
	if briefing := testHandler.OrgDispatchService.DispatchTransferBriefing(context.Background(), parseUUID(setup.issueID)); briefing != "" {
		t.Fatalf("briefing after transfer = %q, want none", briefing)
	}
}
