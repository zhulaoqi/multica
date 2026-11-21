package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestWorkspaceViewerCanManage_Project pins the management capability the
// admin side is gated on. A project is managed by its stored owner/admin rows
// and by the lead of the department that owns it; an ancestor's lead keeps
// content access but does not inherit administration, and a plain member has
// no admin side at all.
func TestWorkspaceViewerCanManage_Project(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	ctx := context.Background()

	projectLead := createPermissionTestMember(t, "manage-project-lead@example.com")
	ancestorLead := createPermissionTestMember(t, "manage-ancestor-lead@example.com")
	otherLead := createPermissionTestMember(t, "manage-other-lead@example.com")
	plain := createPermissionTestMember(t, "manage-plain@example.com")
	owner := createPermissionTestMember(t, "manage-owner@example.com")
	if _, err := testPool.Exec(ctx, `
		UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, owner); err != nil {
		t.Fatalf("promote owner: %v", err)
	}

	var ancestorUnit, owningUnit string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '上级部门', $2) RETURNING id
	`, testWorkspaceID, ancestorLead).Scan(&ancestorUnit); err != nil {
		t.Fatalf("create ancestor unit: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, parent_id, name, lead_user_id)
		VALUES ($1, $2, '归属部门', $3) RETURNING id
	`, testWorkspaceID, ancestorUnit, projectLead).Scan(&owningUnit); err != nil {
		t.Fatalf("create owning unit: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '其他部门', $2)
	`, testWorkspaceID, otherLead); err != nil {
		t.Fatalf("create other unit: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE workspace SET org_unit_id = $2 WHERE id = $1
	`, testWorkspaceID, owningUnit); err != nil {
		t.Fatalf("bind owning department: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET org_unit_id = NULL WHERE id = $1`, testWorkspaceID)
	})

	canManage := func(userID string) bool {
		t.Helper()
		got, err := testHandler.Queries.WorkspaceViewerCanManage(ctx, db.WorkspaceViewerCanManageParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			UserID:      parseUUID(userID),
		})
		if err != nil {
			t.Fatalf("WorkspaceViewerCanManage(%s): %v", userID, err)
		}
		return got.Bool
	}

	cases := []struct {
		name   string
		userID string
		want   bool
	}{
		{"stored owner", owner, true},
		{"owning department lead", projectLead, true},
		{"ancestor department lead", ancestorLead, false},
		{"lead of another department", otherLead, false},
		{"plain member", plain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManage(tc.userID); got != tc.want {
				t.Fatalf("can manage = %v, want %v", got, tc.want)
			}
		})
	}

	// The API exposes the same answer on the workspace payload used by the
	// admin shell and the workspace switcher.
	var got WorkspaceResponse
	req := newRequestAs(projectLead, "GET", "/api/workspaces/"+testWorkspaceID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testutil.Call(t, testHandler.GetWorkspace, req).Want(http.StatusOK).JSON(&got)
	if !got.ViewerCanManage {
		t.Fatal("GetWorkspace: owning department lead must receive viewer_can_manage=true")
	}

	w := testutil.Call(t, testHandler.ListWorkspaces, newRequestAs(plain, "GET", "/api/workspaces", nil)).
		Want(http.StatusOK)
	var list []WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode workspace list: %v", err)
	}
	for _, ws := range list {
		if ws.ID == testWorkspaceID && ws.ViewerCanManage {
			t.Fatal("ListWorkspaces: plain member must not receive viewer_can_manage=true")
		}
	}
}

// TestWorkspaceViewerCanManage_Organization covers the organization workspace:
// the lead of any department manages it, while a member without a lead
// position does not. Membership is removed to prove the capability comes from
// the department tree, not from a stored role.
func TestWorkspaceViewerCanManage_Organization(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	ctx := context.Background()

	orgLead := createPermissionTestMember(t, "manage-org-lead@example.com")
	plain := createPermissionTestMember(t, "manage-org-plain@example.com")
	if _, err := testPool.Exec(ctx, `
		DELETE FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, orgLead); err != nil {
		t.Fatalf("remove lead membership: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '平台部', $2)
	`, testWorkspaceID, orgLead); err != nil {
		t.Fatalf("create department: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE workspace SET is_organization = true WHERE id = $1
	`, testWorkspaceID); err != nil {
		t.Fatalf("mark organization: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET is_organization = false WHERE id = $1`, testWorkspaceID)
	})

	canManage := func(userID string) bool {
		t.Helper()
		got, err := testHandler.Queries.WorkspaceViewerCanManage(ctx, db.WorkspaceViewerCanManageParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			UserID:      parseUUID(userID),
		})
		if err != nil {
			t.Fatalf("WorkspaceViewerCanManage(%s): %v", userID, err)
		}
		return got.Bool
	}
	if !canManage(orgLead) {
		t.Fatal("a department lead must manage the organization workspace")
	}
	if canManage(plain) {
		t.Fatal("a plain member must not manage the organization workspace")
	}

	ids, err := testHandler.Queries.ListManageableWorkspaceIDs(ctx, parseUUID(orgLead))
	if err != nil {
		t.Fatalf("ListManageableWorkspaceIDs: %v", err)
	}
	found := false
	for _, id := range ids {
		if uuidToString(id) == testWorkspaceID {
			found = true
		}
	}
	if !found {
		t.Fatal("ListManageableWorkspaceIDs must include the organization for a department lead")
	}
}
