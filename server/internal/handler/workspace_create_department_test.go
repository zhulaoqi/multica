package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestWorkspaceCreation_RequiresDepartmentLeadership pins the rule that only a
// department lead (or the platform operator) may create a project: the create
// screen offers a plain member no departments, and the create endpoint refuses
// both a missing department and one the caller does not lead.
func TestWorkspaceCreation_RequiresDepartmentLeadership(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	ctx := context.Background()

	lead := createPermissionTestMember(t, "create-dept-lead@example.com")
	otherLead := createPermissionTestMember(t, "create-dept-other-lead@example.com")
	plain := createPermissionTestMember(t, "create-dept-plain@example.com")

	var leadUnit, otherUnit string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '研发部', $2) RETURNING id
	`, testWorkspaceID, lead).Scan(&leadUnit); err != nil {
		t.Fatalf("create lead unit: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '其他部门', $2) RETURNING id
	`, testWorkspaceID, otherLead).Scan(&otherUnit); err != nil {
		t.Fatalf("create other unit: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE workspace SET is_organization = true WHERE id = $1
	`, testWorkspaceID); err != nil {
		t.Fatalf("mark organization: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET is_organization = false WHERE id = $1`, testWorkspaceID)
	})

	options := func(userID string) WorkspaceCreationOptionsResponse {
		t.Helper()
		var got WorkspaceCreationOptionsResponse
		testutil.Call(t, testHandler.ListWorkspaceCreateOptions,
			newRequestAs(userID, "GET", "/api/workspaces/creation-options", nil)).
			Want(http.StatusOK).
			JSON(&got)
		return got
	}

	leadOptions := options(lead)
	if !leadOptions.CanCreate {
		t.Fatal("a department lead must be offered project creation")
	}
	if len(leadOptions.Departments) != 1 || leadOptions.Departments[0].UnitID != leadUnit {
		t.Fatalf("lead departments = %+v, want only %s", leadOptions.Departments, leadUnit)
	}

	plainOptions := options(plain)
	if plainOptions.CanCreate {
		t.Fatal("a plain member must not be offered project creation")
	}
	if len(plainOptions.Departments) != 0 {
		t.Fatalf("plain member departments = %+v, want none", plainOptions.Departments)
	}

	denied := func(body map[string]any, wantCode string) {
		t.Helper()
		got := testutil.Call(t, testHandler.CreateWorkspace,
			newRequestAs(plain, "POST", "/api/workspaces", body)).
			Want(http.StatusForbidden).
			Map()["code"]
		if got != wantCode {
			t.Fatalf("error code = %v, want %q", got, wantCode)
		}
	}

	denied(map[string]any{
		"name": "No Department",
		"slug": "handler-tests-no-department",
	}, codeDepartmentRequired)

	denied(map[string]any{
		"name":        "Other Department",
		"slug":        "handler-tests-other-department",
		"org_unit_id": otherUnit,
	}, codeDepartmentForbidden)
}
