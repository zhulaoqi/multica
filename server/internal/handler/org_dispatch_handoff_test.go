package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// Receipts name who is doing the work. A sub-issue has no origin row of its
// own, so it must resolve to the dispatch its root belongs to.
func TestDispatchHandoff_DepartmentDispatchCoversItsSubIssues(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "handoff-dept@example.com")
	res := seedChatDispatch(t, requester, "回执交给组", "dispatch-test-handoff 部门")
	t.Cleanup(func() { clearDispatchFixtures(t) })
	ctx := context.Background()
	svc := testHandler.OrgDispatchService

	insertIssue := func(title string, parent any) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, parent_issue_id, number)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, $4,
			        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
			RETURNING id::text
		`, testWorkspaceID, title, requester, parent).Scan(&id); err != nil {
			t.Fatalf("insert issue %s: %v", title, err)
		}
		return id
	}
	child := insertIssue("dispatch-test-handoff 子任务", res.Issue.ID)
	plain := insertIssue("dispatch-test-handoff 普通任务", nil)

	origin, ok := svc.DispatchOriginForIssue(ctx, parseUUID(child))
	if !ok || origin.IssueID != parseUUID(res.Issue.ID) {
		t.Fatalf("sub-issue origin = %+v, %v; want the root dispatch", origin, ok)
	}
	want := service.DispatchHandoff{Kind: service.DispatchHandoffDepartment, Name: "回执交给组"}
	if got := svc.HandoffOf(ctx, origin); got != want {
		t.Fatalf("handoff = %+v, want %+v", got, want)
	}
	if name := svc.DispatchLeadAgentName(ctx, loadIssueForChat(t, res.Issue.ID)); name != "回执交给组-leader" {
		t.Fatalf("lead agent = %q, want the squad leader", name)
	}
	if origin, ok := svc.DispatchOriginForIssue(ctx, parseUUID(plain)); ok {
		t.Fatalf("an issue nobody dispatched resolved to %+v", origin)
	}
}

func TestDispatchHandoff_NamesThePersonalSquad(t *testing.T) {
	startDispatchTest(t)
	requester := createOrgTestAdmin(t, "handoff-personal@example.com")
	insertRunnableDutyAgent(t, "handoff-personal-twin", requester)
	t.Cleanup(func() { clearDispatchFixtures(t) })
	ctx := context.Background()
	svc := testHandler.OrgDispatchService

	status, res, body := postDispatchWork(t, requester, map[string]any{
		"text":              "dispatch-test-handoff 小队",
		"client_request_id": "88888888-8888-8888-8888-888888888001",
		"personal":          true,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	origin, ok := svc.DispatchOriginForIssue(ctx, parseUUID(res.Issue.ID))
	if !ok {
		t.Fatal("personal dispatch has no origin")
	}
	// The personal squad is named after the twin that leads it.
	want := service.DispatchHandoff{Kind: service.DispatchHandoffSquad, Name: "handoff-personal-twin"}
	if got := svc.HandoffOf(ctx, origin); got != want {
		t.Fatalf("handoff = %+v, want %+v", got, want)
	}
	if name := svc.DispatchLeadAgentName(ctx, loadIssueForChat(t, res.Issue.ID)); name != "handoff-personal-twin" {
		t.Fatalf("lead agent = %q, want the twin", name)
	}
}

// A project dispatch also records the owning department; the receipt names
// the project.
func TestDispatchHandoff_NamesTheProjectBeforeItsDepartment(t *testing.T) {
	setup := setupProjectDispatch(t, "handoff-project", "回执项目", true)
	ctx := context.Background()
	svc := testHandler.OrgDispatchService

	status, res, body := postDispatchWork(t, setup.requester, map[string]any{
		"text":              "dispatch-test-handoff 项目",
		"client_request_id": "88888888-8888-8888-8888-888888888002",
		"project_id":        setup.projectID,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	origin, ok := svc.DispatchOriginForIssue(ctx, parseUUID(res.Issue.ID))
	if !ok || !origin.TargetUnitID.Valid {
		t.Fatalf("project dispatch origin = %+v, %v", origin, ok)
	}
	want := service.DispatchHandoff{Kind: service.DispatchHandoffProject, Name: "回执项目"}
	if got := svc.HandoffOf(ctx, origin); got != want {
		t.Fatalf("handoff = %+v, want %+v", got, want)
	}
	if name := svc.DispatchLeadAgentName(ctx, loadIssueForChat(t, res.Issue.ID)); name != "handoff-project-project-agent" {
		t.Fatalf("lead agent = %q, want the project executor", name)
	}
}
