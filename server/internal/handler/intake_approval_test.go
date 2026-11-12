package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListMyIntakeRequests_ShowsApprovalStep(t *testing.T) {
	requireDB(t)
	seedTestCatalog(t)
	lead := createPermissionTestMember(t, "intake-approval-lead@example.com")
	owner := createPermissionTestMember(t, "intake-approval-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	wf := createTestWorkflow(t, "Intake approval "+workflowTestSuffix(), "in_progress", "in_review")
	setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{
		"handler": map[string]any{"type": "project_lead"}, "instructions": "批准上线",
	}).Want(http.StatusOK)
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	agentID := insertIntakeDutyAgent(t, "intake-approval-agent", owner)
	issueID := insertIntakeIssue(t, projectID, agentID, lead)
	moduleID := insertPlainModule(t, projectID, agentID, "")
	if _, err := testHandler.Queries.CreateIntakeRequest(context.Background(), db.CreateIntakeRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), IssueID: parseUUID(issueID), ProjectID: parseUUID(projectID),
		ModuleID: parseUUID(moduleID), RequesterID: parseUUID(lead), ClientRequestID: parseUUID(uuid.NewString()),
		SourceBody: "做完等负责人批准",
	}); err != nil {
		t.Fatal(err)
	}

	if got := myIntakeDisplayStatus(t, lead, issueID); got != "等待处理" {
		t.Fatalf("todo display = %s", got)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID); err != nil {
		t.Fatal(err)
	}
	if got := myIntakeDisplayStatus(t, lead, issueID); got != "待审批" {
		t.Fatalf("approval step display = %s, want 待审批", got)
	}
}

func myIntakeDisplayStatus(t *testing.T, userID, issueID string) string {
	t.Helper()
	req := newRequest("GET", "/api/intake/requests?mine=1", nil)
	req.Header.Set("X-User-ID", userID)
	var items []struct {
		IssueID string `json:"issue_id"`
		Status  string `json:"display_status"`
	}
	testutil.Call(t, testHandler.ListMyIntakeRequests, req).Want(http.StatusOK).JSON(&items)
	for _, item := range items {
		if item.IssueID == issueID {
			return item.Status
		}
	}
	t.Fatalf("issue %s not in list %+v", issueID, items)
	return ""
}

func TestIntakeDisplayStatus_AwaitingApproval(t *testing.T) {
	if got := intakeDisplayStatus("release_approval", false, true, "", "", ""); got != "待审批" {
		t.Fatalf("approval step = %s", got)
	}
	if got := intakeDisplayStatus("release_approval", true, true, "", "", ""); got != "等你补充" {
		t.Fatalf("open blocker should win, got %s", got)
	}
	if got := intakeDisplayStatus("in_review", false, true, "", "", ""); got != "待验收" {
		t.Fatalf("in_review should stay acceptance, got %s", got)
	}
	if got := intakeDisplayStatus("qa", false, false, "running", "", ""); got != "正在做" {
		t.Fatalf("agent step = %s", got)
	}
}
