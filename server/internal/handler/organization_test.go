package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createOrganizationProject inserts a project owned by unitID and returns its
// id. Rows the test adds to the project are removed with it.
func createOrganizationProject(t *testing.T, slug, unitID string) string {
	t.Helper()
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix, org_unit_id)
		VALUES ($1, $1, 'ORG', $2)
		RETURNING id::text
	`, slug, unitID).Scan(&projectID); err != nil {
		t.Fatalf("create project %s: %v", slug, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, projectID)
	})
	return projectID
}

func organizationUnitID(t *testing.T, externalID string) string {
	t.Helper()
	var unitID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id::text FROM org_unit WHERE workspace_id = $1 AND external_id = $2
	`, testWorkspaceID, externalID).Scan(&unitID); err != nil {
		t.Fatalf("read unit %s: %v", externalID, err)
	}
	return unitID
}

func userIDByEmail(t *testing.T, email string) string {
	t.Helper()
	var userID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id::text FROM "user" WHERE lower(email) = lower($1)
	`, email).Scan(&userID); err != nil {
		t.Fatalf("read user %s: %v", email, err)
	}
	return userID
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func decodeErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v: %s", err, rec.Body.String())
	}
	return body.Code
}

func TestSyncOrgFromFeishu_RefusesOutsideOrganizationWorkspace(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	admin := createOrgTestAdmin(t, "org-sync-project@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	var orgID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, issue_prefix, is_organization)
		VALUES ('Organization elsewhere', 'organization-elsewhere', 'ORE', true)
		RETURNING id::text
	`).Scan(&orgID); err != nil {
		t.Fatalf("create organization workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, orgID)
	})

	rec := httptest.NewRecorder()
	testHandler.SyncOrgFromFeishu(rec, operatorRequest(admin, http.MethodPost, "/api/org/sync/feishu", nil))
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeOrganizationManaged {
		t.Fatalf("project sync = %d %s, want 409 %s", rec.Code, rec.Body.String(), codeOrganizationManaged)
	}
	if count := countRows(t, `SELECT count(*) FROM org_unit WHERE workspace_id = $1`, testWorkspaceID); count != 0 {
		t.Fatalf("project sync wrote %d units, want none", count)
	}
}

func TestSyncOrgFromFeishu_RosterTakesNoSeats(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "org-sync-seats@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	const intents = `SELECT count(*) FROM seat_capacity_outbox WHERE workspace_id = $1`
	before := countRows(t, intents, testWorkspaceID)
	if code, report := syncOrg(t, admin); code != http.StatusOK || len(report.MembersAdded) != 2 {
		t.Fatalf("sync = %d, members added %+v", code, report.MembersAdded)
	}
	if after := countRows(t, intents, testWorkspaceID); after != before {
		t.Fatalf("seat intents went from %d to %d; roster rows must not take seats", before, after)
	}
}

func TestSyncOrgFromFeishu_ProjectsFollowOrganization(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "org-sync-follow@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	if code, _ := syncOrg(t, admin); code != http.StatusOK {
		t.Fatalf("initial sync status = %d", code)
	}
	engID := organizationUnitID(t, "od-eng")
	payID := organizationUnitID(t, "od-pay")
	projectID := createOrganizationProject(t, "org-follow-project", engID)
	ada := userIDByEmail(t, "ada@example.com")
	lead := userIDByEmail(t, "lead@example.com")
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, projectID, ada); err != nil {
		t.Fatalf("add ada to project: %v", err)
	}

	if code, _ := syncOrg(t, admin); code != http.StatusOK {
		t.Fatalf("second sync status = %d", code)
	}

	var placed string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(org_unit_id::text, '') FROM member WHERE workspace_id = $1 AND user_id = $2
	`, projectID, ada).Scan(&placed); err != nil {
		t.Fatalf("read project placement: %v", err)
	}
	if placed != payID {
		t.Fatalf("project placement = %q, want the organization unit %q", placed, payID)
	}

	var role string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2
	`, projectID, lead).Scan(&role); err != nil {
		t.Fatalf("owning department lead has no project row: %v", err)
	}
	if role != "owner" {
		t.Fatalf("owning department lead role = %q, want owner", role)
	}
}

func TestOrgUnitEdits_ProjectsDeferToOrganization(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "org-project-edit@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")
	if code, _ := syncOrg(t, admin); code != http.StatusOK {
		t.Fatalf("sync status = %d", code)
	}
	projectID := createOrganizationProject(t, "org-edit-project", organizationUnitID(t, "od-eng"))
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, projectID, admin); err != nil {
		t.Fatalf("add admin to project: %v", err)
	}

	req := operatorRequest(admin, http.MethodPost, "/api/org/units", map[string]any{"name": "项目里的新部门"})
	req.Header.Set("X-Workspace-ID", projectID)
	rec := httptest.NewRecorder()
	testHandler.CreateOrgUnit(rec, req)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeOrganizationManaged {
		t.Fatalf("project unit create = %d %s, want 409 %s", rec.Code, rec.Body.String(), codeOrganizationManaged)
	}
}

func TestOrganizationWorkspace_RefusesInvitationsAndShareLinks(t *testing.T) {
	requireDB(t)
	markOrganizationWorkspace(t)
	clearInvitationsForTestWorkspace(t)

	invite := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: "org-invite@example.com",
		Role:  "member",
	}), "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.CreateInvitation(rec, invite)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeDirectAddOnly {
		t.Fatalf("organization invitation = %d %s, want 409 %s", rec.Code, rec.Body.String(), codeDirectAddOnly)
	}

	link := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{
		Role: "member",
	}), "id", testWorkspaceID)
	rec = httptest.NewRecorder()
	testHandler.CreateShareLink(rec, link)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeDirectAddOnly {
		t.Fatalf("organization share link = %d %s, want 409 %s", rec.Code, rec.Body.String(), codeDirectAddOnly)
	}
}
