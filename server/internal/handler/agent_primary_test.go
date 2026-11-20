package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func primaryFlag(t *testing.T, agentID string) bool {
	t.Helper()
	var primary bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT is_primary FROM agent WHERE id = $1
	`, agentID).Scan(&primary); err != nil {
		t.Fatalf("load primary flag: %v", err)
	}
	return primary
}

func TestCreateIntakeAgent_MarksFirstAgentPrimary(t *testing.T) {
	requireDB(t)
	owner := createPermissionTestMember(t, "primary-first@example.com")
	first := insertIntakeDutyAgent(t, "primary-first-agent", owner)
	second := insertIntakeDutyAgent(t, "primary-second-agent", owner)

	if !primaryFlag(t, first) {
		t.Fatal("first agent should be the owner's primary twin")
	}
	if primaryFlag(t, second) {
		t.Fatal("second agent must not displace the existing primary")
	}
}

func TestSetPrimaryAgent_SwitchesWithinOwner(t *testing.T) {
	requireDB(t)
	owner := createPermissionTestMember(t, "primary-switch@example.com")
	first := insertIntakeDutyAgent(t, "primary-switch-first", owner)
	second := insertIntakeDutyAgent(t, "primary-switch-second", owner)

	rec := httptest.NewRecorder()
	testHandler.SetPrimaryAgent(rec, orgRequestWithParam(owner, http.MethodPut, "/api/agents/x/primary", "id", second, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !res.IsPrimary {
		t.Fatal("response should mark the new primary")
	}
	if primaryFlag(t, first) {
		t.Fatal("previous primary must be cleared")
	}
	if !primaryFlag(t, second) {
		t.Fatal("new primary must be stored")
	}
}

func TestSetPrimaryAgent_RejectsAnAgentsNotOwnedByCaller(t *testing.T) {
	requireDB(t)
	owner := createPermissionTestMember(t, "primary-owner@example.com")
	intruder := createPermissionTestMember(t, "primary-intruder@example.com")
	agentID := insertIntakeDutyAgent(t, "primary-foreign", owner)

	rec := httptest.NewRecorder()
	testHandler.SetPrimaryAgent(rec, orgRequestWithParam(intruder, http.MethodPut, "/api/agents/x/primary", "id", agentID, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !primaryFlag(t, agentID) {
		t.Fatal("a rejected request must not change the primary flag")
	}
}

func TestArchivePrimaryAgent_PromotesAnotherOwnedAgent(t *testing.T) {
	requireDB(t)
	owner := createPermissionTestMember(t, "primary-archive@example.com")
	first := insertIntakeDutyAgent(t, "primary-archive-first", owner)
	second := insertIntakeDutyAgent(t, "primary-archive-second", owner)
	if !primaryFlag(t, first) {
		t.Fatal("fixture should start with the first agent primary")
	}

	rec := httptest.NewRecorder()
	testHandler.ArchiveAgent(rec, orgRequestWithParam(owner, http.MethodDelete, "/api/agents/x", "id", first, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if primaryFlag(t, first) {
		t.Fatal("archived agent must not stay primary")
	}
	if !primaryFlag(t, second) {
		t.Fatal("next owned agent should be promoted so the member stays bound")
	}
}

func TestEnsurePrimaryTwinInWorkspace_ClonesAcrossProjects(t *testing.T) {
	requireDB(t)
	owner := createPermissionTestMember(t, "primary-clone@example.com")
	source := insertIntakeDutyAgent(t, "primary-clone-source", owner)

	var target string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, issue_prefix, org_unit_id)
		VALUES ('Primary clone target', 'primary-clone-target', 'PCT', NULL)
		RETURNING id::text
	`).Scan(&target); err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1`, target)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, target)
	})

	if err := testHandler.ensurePrimaryTwinInWorkspace(context.Background(), parseUUID(target), parseUUID(owner), pgtype.UUID{}); err != nil {
		t.Fatalf("ensure primary twin: %v", err)
	}

	var clonedID string
	var primary bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT id::text, is_primary FROM agent
		WHERE workspace_id = $1 AND owner_id = $2 AND archived_at IS NULL
	`, target, owner).Scan(&clonedID, &primary); err != nil {
		t.Fatalf("load cloned twin: %v", err)
	}
	if clonedID == source {
		t.Fatal("clone must be a distinct row in the target workspace")
	}
	if !primary {
		t.Fatal("cloned twin must be the primary agent in the new project")
	}
}

func TestCreateIssue_AssigningMemberRequiresPrimaryTwin(t *testing.T) {
	requireDB(t)
	assignee := createPermissionTestMember(t, "primary-assignee@example.com")
	create := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assigned to a person",
			"assignee_type": "member",
			"assignee_id":   assignee,
		}))
		return rec
	}

	if rec := create(); rec.Code != http.StatusForbidden {
		t.Fatalf("unbound assignee: status = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	insertIntakeDutyAgent(t, "primary-assignee-twin", assignee)
	rec := create()
	if rec.Code != http.StatusCreated {
		t.Fatalf("bound assignee: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var issue IssueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
}
