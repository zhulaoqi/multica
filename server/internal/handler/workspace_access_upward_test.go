package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestWorkspaceAccess_UpwardInheritanceAndCrossProjectMember covers the two
// access rules that make a project usable beyond its own department:
//   - a lead of an ancestor department inherits content access without a
//     member row, but does not manage the project;
//   - an explicit member row from another department keeps access.
func TestWorkspaceAccess_UpwardInheritanceAndCrossProjectMember(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)

	ctx := context.Background()
	parentLead := createPermissionTestMember(t, "upward-parent-lead@example.com")
	childLead := createPermissionTestMember(t, "upward-child-lead@example.com")
	outsider := createPermissionTestMember(t, "upward-outsider@example.com")
	// The ancestor lead has no member row; access is purely inherited.
	if _, err := testPool.Exec(ctx, `
		DELETE FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, parentLead); err != nil {
		t.Fatalf("remove parent lead membership: %v", err)
	}

	var parentID, childID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, lead_user_id) VALUES ($1, '父部门', $2) RETURNING id
	`, testWorkspaceID, parentLead).Scan(&parentID); err != nil {
		t.Fatalf("create parent unit: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, parent_id, name, lead_user_id)
		VALUES ($1, $2, '子部门', $3) RETURNING id
	`, testWorkspaceID, parentID, childLead).Scan(&childID); err != nil {
		t.Fatalf("create child unit: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, outsider, parentID); err != nil {
		t.Fatalf("place cross-project member: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE workspace SET org_unit_id = $2 WHERE id = $1
	`, testWorkspaceID, childID); err != nil {
		t.Fatalf("bind workspace department: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET org_unit_id = NULL WHERE id = $1`, testWorkspaceID)
	})

	outside := createPermissionTestMember(t, "upward-stranger@example.com")
	if _, err := testPool.Exec(ctx, `
		DELETE FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, outside); err != nil {
		t.Fatalf("remove stranger membership: %v", err)
	}

	access := func(userID string) workspaceaccess.Access {
		t.Helper()
		got, err := workspaceaccess.Resolve(ctx, testHandler.Queries, parseUUID(testWorkspaceID), parseUUID(userID), false)
		if err != nil {
			t.Fatalf("resolve access for %s: %v", userID, err)
		}
		return got
	}

	parent := access(parentLead)
	if !parent.Allowed() || !parent.Implicit() || parent.IsManager() {
		t.Fatalf("parent lead access = %+v, want implicit content access without management", parent)
	}
	if parent.HasMember {
		t.Fatal("parent lead should not have a member row")
	}

	child := access(childLead)
	if !child.Allowed() || !child.IsOrgLead() || !child.IsManager() {
		t.Fatalf("owning department lead access = %+v, want content and management", child)
	}

	cross := access(outsider)
	if !cross.Allowed() || cross.InSubtree {
		t.Fatalf("cross-project member access = %+v, want allowed outside the subtree", cross)
	}

	if access(outside).Allowed() {
		t.Fatal("a user with no member row and no lead position must not read the project")
	}

	workspaces, err := testHandler.Queries.ListWorkspaces(ctx, parseUUID(parentLead))
	if err != nil {
		t.Fatalf("list workspaces for ancestor lead: %v", err)
	}
	found := false
	for _, ws := range workspaces {
		if uuidToString(ws.ID) == testWorkspaceID {
			found = true
		}
	}
	if !found {
		t.Fatal("ancestor lead does not see the child department's project in ListWorkspaces")
	}

	strangerWorkspaces, err := testHandler.Queries.ListWorkspaces(ctx, parseUUID(outside))
	if err != nil {
		t.Fatalf("list workspaces for stranger: %v", err)
	}
	for _, ws := range strangerWorkspaces {
		if uuidToString(ws.ID) == testWorkspaceID {
			t.Fatal("stranger must not see the project")
		}
	}

	projectMembers, err := testHandler.Queries.ListProjectMemberUserIDs(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("list project members: %v", err)
	}
	seen := make(map[string]bool, len(projectMembers))
	for _, userID := range projectMembers {
		seen[uuidToString(userID)] = true
	}
	for name, userID := range map[string]string{
		"ancestor lead": parentLead,
		"owning lead":   childLead,
		"cross-project": outsider,
	} {
		if !seen[userID] {
			t.Fatalf("project member list is missing %s", name)
		}
	}
	if seen[outside] {
		t.Fatal("project member list must not include a stranger")
	}

	realtime, err := testHandler.Queries.GetRealtimeMemberships(ctx, db.GetRealtimeMembershipsParams{
		WorkspaceIds: []pgtype.UUID{parseUUID(testWorkspaceID), parseUUID(testWorkspaceID), parseUUID(testWorkspaceID)},
		UserIds:      []pgtype.UUID{parseUUID(parentLead), parseUUID(outsider), parseUUID(outside)},
	})
	if err != nil {
		t.Fatalf("resolve realtime memberships: %v", err)
	}
	realtimeSeen := make(map[string]bool, len(realtime))
	for _, row := range realtime {
		realtimeSeen[uuidToString(row.UserID)] = true
	}
	if !realtimeSeen[parentLead] {
		t.Fatal("realtime fanout must include the inherited ancestor lead")
	}
	if !realtimeSeen[outsider] {
		t.Fatal("realtime fanout must include an explicit cross-project member")
	}
	if realtimeSeen[outside] {
		t.Fatal("realtime fanout must not include a stranger")
	}

	rec := httptest.NewRecorder()
	testHandler.ListMembersWithUser(rec, withURLParam(
		newRequestAs(parentLead, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/members", nil),
		"id", testWorkspaceID,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("member roster status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var roster []MemberWithUserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &roster); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	implicitRow := false
	for _, member := range roster {
		if member.UserID == parentLead && member.ImplicitAccess {
			implicitRow = true
		}
	}
	if !implicitRow {
		t.Fatal("ancestor lead should receive a synthetic implicit member row")
	}
}
