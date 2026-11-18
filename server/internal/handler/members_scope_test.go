package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListMembersWithUser_ScopesToDepartment guards the roster boundary: the
// company directory is operator-owned, so a workspace owner only reads people
// in departments they belong to or lead.
func TestListMembersWithUser_ScopesToDepartment(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	teammate := createPermissionTestMember(t, "members-scope-teammate@example.com")
	outsider := createPermissionTestMember(t, "members-scope-outsider@example.com")

	ctx := context.Background()
	var alphaID, betaID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name) VALUES ($1, '部门 A') RETURNING id
	`, testWorkspaceID).Scan(&alphaID); err != nil {
		t.Fatalf("create unit A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name) VALUES ($1, '部门 B') RETURNING id
	`, testWorkspaceID).Scan(&betaID); err != nil {
		t.Fatalf("create unit B: %v", err)
	}
	place := func(userID, unitID string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, userID, unitID); err != nil {
			t.Fatalf("place %s: %v", userID, err)
		}
	}
	place(testUserID, alphaID)
	place(teammate, alphaID)
	place(outsider, betaID)
	t.Cleanup(func() {
		for _, userID := range []string{testUserID, teammate, outsider} {
			testPool.Exec(context.Background(), `
				UPDATE member SET org_unit_id = NULL WHERE workspace_id = $1 AND user_id = $2
			`, testWorkspaceID, userID)
		}
	})

	list := func(req *http.Request) map[string]bool {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.ListMembersWithUser(rec, withURLParam(req, "id", testWorkspaceID))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var resp []MemberWithUserResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		seen := make(map[string]bool, len(resp))
		for _, member := range resp {
			seen[member.UserID] = true
		}
		return seen
	}

	owner := list(newRequestAs(testUserID, "GET", "/api/workspaces/"+testWorkspaceID+"/members", nil))
	if !owner[testUserID] || !owner[teammate] {
		t.Fatalf("owner roster = %v, want themselves and their department", owner)
	}
	if owner[outsider] {
		t.Fatalf("owner roster = %v, must not include another department", owner)
	}

	directory := list(operatorRequest(testUserID, "GET", "/api/workspaces/"+testWorkspaceID+"/members", nil))
	for name, userID := range map[string]string{
		"owner": testUserID, "teammate": teammate, "outsider": outsider,
	} {
		if !directory[userID] {
			t.Fatalf("operator roster is missing %s", name)
		}
	}
}
