package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/integrations/feishu/contact"
)

type fakeOrgDirectory struct {
	departments []contact.Department
	usersByUnit map[string][]contact.User
}

func (f *fakeOrgDirectory) ListDepartments(context.Context) ([]contact.Department, error) {
	return f.departments, nil
}

func (f *fakeOrgDirectory) ListDepartmentUsers(_ context.Context, unitIDs []string) ([]contact.User, error) {
	var users []contact.User
	for _, id := range unitIDs {
		users = append(users, f.usersByUnit[id]...)
	}
	return users, nil
}

// installOrgDirectory swaps the handler's directory for one test and removes
// the rows the sync creates.
func installOrgDirectory(t *testing.T, directory OrgDirectory, emails ...string) {
	t.Helper()
	previous := testHandler.OrgDirectory
	testHandler.OrgDirectory = directory
	t.Cleanup(func() {
		testHandler.OrgDirectory = previous
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM org_sync_run WHERE workspace_id = $1`, testWorkspaceID)
		for _, email := range emails {
			testPool.Exec(ctx, `DELETE FROM feishu_identity WHERE lower(email) = lower($1)`, email)
			testPool.Exec(ctx, `
				DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE lower(email) = lower($1))
			`, email)
			testPool.Exec(ctx, `DELETE FROM "user" WHERE lower(email) = lower($1)`, email)
		}
	})
}

// markOrganizationWorkspace makes the shared test workspace the organization
// workspace for one test. Directory sync only writes there.
func markOrganizationWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET is_organization = true WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("mark organization workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET is_organization = false WHERE id = $1`, testWorkspaceID)
	})
}

func directoryFixture() *fakeOrgDirectory {
	return &fakeOrgDirectory{
		departments: []contact.Department{
			// Children are listed first on purpose: the upsert must order by depth.
			{ID: "od-pay", ParentID: "od-eng", Name: "支付组"},
			{ID: "od-eng", ParentID: "", Name: "平台部", LeadOpenID: "ou-lead"},
		},
		usersByUnit: map[string][]contact.User{
			"od-eng": {{OpenID: "ou-lead", Name: "Lead Person", Email: "Lead@Example.com", DepartmentID: "od-eng"}},
			"od-pay": {
				{OpenID: "ou-ada", Name: "Ada", Email: "ada@example.com", DepartmentID: "od-pay"},
				{OpenID: "ou-nomail", Name: "No Mail", DepartmentID: "od-pay"},
			},
		},
	}
}

func syncOrg(t *testing.T, admin string) (int, orgSyncReport) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.SyncOrgFromFeishu(rec, operatorRequest(admin, "POST", "/api/org/sync/feishu", nil))
	var report orgSyncReport
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
			t.Fatalf("decode sync report: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, report
}

func TestSyncOrgFromFeishu_ImportsTreePeopleAndLeads(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "org-sync-admin@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	code, report := syncOrg(t, admin)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(report.CreatedUnits) != 2 || report.CreatedUnits[0] != "平台部" || report.CreatedUnits[1] != "支付组" {
		t.Fatalf("created units = %+v, want parents first", report.CreatedUnits)
	}
	if len(report.MembersAdded) != 2 {
		t.Fatalf("members added = %+v, want Lead Person and Ada", report.MembersAdded)
	}
	if len(report.MembersWithoutEmail) != 1 || report.MembersWithoutEmail[0] != "No Mail" {
		t.Fatalf("members without email = %+v", report.MembersWithoutEmail)
	}
	if len(report.LeadsAssigned) != 1 || report.LeadsAssigned[0] != "平台部" {
		t.Fatalf("leads assigned = %+v", report.LeadsAssigned)
	}

	var payUnitID, engLeadID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id::text FROM org_unit WHERE workspace_id = $1 AND external_id = 'od-pay'
	`, testWorkspaceID).Scan(&payUnitID); err != nil {
		t.Fatalf("read payments unit: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(lead_user_id::text, '') FROM org_unit
		WHERE workspace_id = $1 AND external_id = 'od-eng'
	`, testWorkspaceID).Scan(&engLeadID); err != nil {
		t.Fatalf("read platform lead: %v", err)
	}
	if engLeadID == "" {
		t.Fatal("platform unit lead should come from the directory leader")
	}

	var adaPlaced string
	if err := testPool.QueryRow(context.Background(), `
		SELECT m.org_unit_id::text
		FROM member m JOIN "user" u ON u.id = m.user_id
		WHERE m.workspace_id = $1 AND lower(u.email) = 'ada@example.com'
	`, testWorkspaceID).Scan(&adaPlaced); err != nil {
		t.Fatalf("read ada placement: %v", err)
	}
	if adaPlaced != payUnitID {
		t.Fatalf("ada unit = %q, want %q", adaPlaced, payUnitID)
	}

	// Re-running with the same directory must not create or move anything.
	code, again := syncOrg(t, admin)
	if code != http.StatusOK {
		t.Fatalf("second sync status = %d", code)
	}
	if len(again.CreatedUnits)+len(again.MembersAdded)+len(again.MembersMoved)+len(again.UpdatedUnits)+len(again.LeadsAssigned) != 0 {
		t.Fatalf("idempotent sync changed state: %+v", again)
	}
}

func TestSyncOrgFromFeishu_ReportsDetachedAndMovesPeople(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "org-sync-detach@example.com")
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	if code, _ := syncOrg(t, admin); code != http.StatusOK {
		t.Fatalf("initial sync status = %d", code)
	}

	// The directory drops the payments team and moves Ada into the parent.
	shrunk := &fakeOrgDirectory{
		departments: []contact.Department{{ID: "od-eng", ParentID: "", Name: "平台研发中心"}},
		usersByUnit: map[string][]contact.User{
			"od-eng": {{OpenID: "ou-ada", Name: "Ada", Email: "ada@example.com", DepartmentID: "od-eng"}},
		},
	}
	testHandler.OrgDirectory = shrunk

	code, report := syncOrg(t, admin)
	if code != http.StatusOK {
		t.Fatalf("second sync status = %d", code)
	}
	if len(report.UpdatedUnits) != 1 || report.UpdatedUnits[0] != "平台研发中心" {
		t.Fatalf("updated units = %+v", report.UpdatedUnits)
	}
	if len(report.MembersMoved) != 1 || report.MembersMoved[0] != "Ada" {
		t.Fatalf("members moved = %+v", report.MembersMoved)
	}
	if len(report.DetachedDepartments) != 1 || report.DetachedDepartments[0] != "支付组" {
		t.Fatalf("detached departments = %+v, want the dropped 支付组", report.DetachedDepartments)
	}
	if len(report.DetachedUsers) != 1 || report.DetachedUsers[0] != "lead@example.com" {
		t.Fatalf("detached users = %+v, want the person who left the directory", report.DetachedUsers)
	}
}

func TestSyncOrgFromFeishu_RequiresOperator(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	member := createPermissionTestMember(t, "org-sync-member@example.com")
	admin := createOrgTestAdmin(t, "org-sync-admin@example.com")
	installOrgDirectory(t, directoryFixture())

	for name, caller := range map[string]string{"member": member, "workspace admin": admin} {
		rec := httptest.NewRecorder()
		testHandler.SyncOrgFromFeishu(rec, newRequestAs(caller, "POST", "/api/org/sync/feishu", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s sync status = %d, want 403", name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	testHandler.GetOrgSyncStatus(rec, newRequestAs(member, "GET", "/api/org/sync/feishu", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("member status read = %d, want 200", rec.Code)
	}
	var status orgSyncStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Configured {
		t.Fatal("configured = false, want true with a directory installed")
	}
}

func TestSyncOrgFromFeishu_AllowsOperator(t *testing.T) {
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
	installOrgDirectory(t, directoryFixture(), "lead@example.com", "ada@example.com")

	req := newRequest("POST", "/api/org/sync/feishu", nil)
	req.Header.Set(auth.OperatorHeader, "1")
	rec := httptest.NewRecorder()
	testHandler.SyncOrgFromFeishu(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator sync status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestSyncOrgFromFeishu_NotConfigured(t *testing.T) {
	requireDB(t)
	testHandler.OrgDirectory = nil
	rec := httptest.NewRecorder()
	testHandler.SyncOrgFromFeishu(rec, operatorRequest(testUserID, "POST", "/api/org/sync/feishu", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}
