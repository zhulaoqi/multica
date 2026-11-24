package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// projectRequest calls a project-scoped handler: the workspace header selects
// the project, while the user header selects the caller.
func projectIntakeRequest(userID, projectID, method, path string, body any) *http.Request {
	req := newRequestAs(userID, method, path, body)
	req.Header.Set("X-Workspace-ID", projectID)
	return req
}

// startProjectIntakeTest prepares the organization workspace and removes the
// project list rows this test creates, since workspace_intake_source has no
// foreign key to clean them up with the workspace.
func startProjectIntakeTest(t *testing.T) {
	t.Helper()
	requireDB(t)
	cleanupOrgUnits(t)
	markOrganizationWorkspace(t)
}

func joinProject(t *testing.T, projectID, userID, role string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
	`, projectID, userID, role); err != nil {
		t.Fatalf("join project %s as %s: %v", projectID, role, err)
	}
}

func addWorkspaceIntakeSource(t *testing.T, projectID, sourceType, sourceID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_intake_source (workspace_id, source_type, source_id)
		VALUES ($1, $2, $3)
	`, projectID, sourceType, sourceID); err != nil {
		t.Fatalf("add project intake source: %v", err)
	}
}

func countWorkspaceIntakeSources(t *testing.T, projectID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_intake_source WHERE workspace_id = $1`, projectID,
	).Scan(&n); err != nil {
		t.Fatalf("count project intake sources: %v", err)
	}
	return n
}

type projectIntakeSourcesResult struct {
	Sources []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"sources"`
	Inherited bool   `json:"inherited"`
	UnitID    string `json:"unit_id"`
	UnitPath  string `json:"unit_path"`
}

func getProjectIntakeSources(t *testing.T, userID, projectID string) (int, projectIntakeSourcesResult, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.GetProjectIntakeSources(rec, projectIntakeRequest(userID, projectID, "GET", "/api/project-intake/sources", nil))
	var res projectIntakeSourcesResult
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode project intake sources: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res, rec.Body.String()
}

func putProjectIntakeSources(t *testing.T, userID, projectID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ReplaceProjectIntakeSources(rec, projectIntakeRequest(userID, projectID, "PUT", "/api/project-intake/sources", body))
	return rec
}

// TestProjectIntakeSources_ManagerConfiguresProjectList covers the project
// list itself: its manager reads and replaces it, plain members only read it,
// and anything that is not a department-owned project answers 409.
func TestProjectIntakeSources_ManagerConfiguresProjectList(t *testing.T) {
	startProjectIntakeTest(t)
	admin := createOrgTestAdmin(t, "proj-intake-admin@example.com")
	manager := createPermissionTestMember(t, "proj-intake-manager@example.com")
	plain := createPermissionTestMember(t, "proj-intake-plain@example.com")
	outsider := createPermissionTestMember(t, "proj-intake-outsider@example.com")
	unitID := createOrgUnitAs(t, admin, "项目归属部", "")
	sourceUnitID := createOrgUnitAs(t, admin, "提需求来源部", "")
	project := createOrganizationProject(t, "proj-intake-manager", unitID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, project)
	})
	joinProject(t, project, manager, "owner")
	joinProject(t, project, plain, "member")

	status, res, body := getProjectIntakeSources(t, manager, project)
	if status != http.StatusOK {
		t.Fatalf("manager get status = %d: %s", status, body)
	}
	if !res.Inherited || len(res.Sources) != 0 {
		t.Fatalf("initial sources = %+v, want inherited and empty", res)
	}
	if res.UnitID != unitID || res.UnitPath != "项目归属部" {
		t.Fatalf("owning unit = %s/%q, want %s", res.UnitID, res.UnitPath, unitID)
	}

	// A plain project member may read the list (the settings page shows it
	// read-only) but cannot change it; a non-member is outside the project
	// entirely and gets the same answer as a missing workspace.
	if status, _, body := getProjectIntakeSources(t, plain, project); status != http.StatusOK {
		t.Fatalf("member get status = %d: %s", status, body)
	}
	if rec := putProjectIntakeSources(t, plain, project, map[string]any{"sources": []map[string]any{}}); rec.Code != http.StatusForbidden {
		t.Fatalf("member put status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if status, _, body := getProjectIntakeSources(t, outsider, project); status != http.StatusNotFound {
		t.Fatalf("outsider get status = %d, want 404: %s", status, body)
	}

	// The manager replaces the whole list with a person and a department.
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": []map[string]any{
		{"type": "user", "id": plain},
		{"type": "org_unit", "id": sourceUnitID},
	}}); rec.Code != http.StatusNoContent {
		t.Fatalf("manager put status = %d: %s", rec.Code, rec.Body.String())
	}
	status, res, body = getProjectIntakeSources(t, manager, project)
	if status != http.StatusOK {
		t.Fatalf("manager get after put status = %d: %s", status, body)
	}
	if res.Inherited || len(res.Sources) != 2 {
		t.Fatalf("sources after put = %+v, want two explicit sources", res)
	}
	want := map[string]bool{"user:" + plain: true, "org_unit:" + sourceUnitID: true}
	for _, source := range res.Sources {
		key := source.Type + ":" + source.ID
		if !want[key] {
			t.Fatalf("unexpected source %q in %+v", key, res.Sources)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing sources %v", want)
	}

	// Validation: unknown type, non-member user, foreign department.
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": []map[string]any{{"type": "team", "id": plain}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad type status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": []map[string]any{{"type": "user", "id": "00000000-0000-0000-0000-000000000000"}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-member status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": []map[string]any{{"type": "org_unit", "id": "00000000-0000-0000-0000-000000000000"}}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown unit status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	tooMany := make([]map[string]any, 201)
	for i := range tooMany {
		tooMany[i] = map[string]any{"type": "user", "id": plain}
	}
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": tooMany}); rec.Code != http.StatusBadRequest {
		t.Fatalf("too many status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// Replacing with an empty list restores the department's rule.
	if rec := putProjectIntakeSources(t, manager, project, map[string]any{"sources": []map[string]any{}}); rec.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d: %s", rec.Code, rec.Body.String())
	}
	if n := countWorkspaceIntakeSources(t, project); n != 0 {
		t.Fatalf("sources after clear = %d, want 0", n)
	}
	if status, res, body = getProjectIntakeSources(t, manager, project); status != http.StatusOK || !res.Inherited {
		t.Fatalf("after clear status = %d inherited = %v: %s", status, res.Inherited, body)
	}

	// The organization itself has no owning department to hand work to.
	rec := httptest.NewRecorder()
	testHandler.GetProjectIntakeSources(rec, newRequestAs(admin, "GET", "/api/project-intake/sources", nil))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "project_intake_unavailable") {
		t.Fatalf("org workspace status = %d, body = %s, want 409 project_intake_unavailable", rec.Code, rec.Body.String())
	}
}

// TestProjectIntakeCandidates_ListOrganizationTreeAndMembers backs the picker:
// a project may pull in colleagues and departments from the whole company.
func TestProjectIntakeCandidates_ListOrganizationTreeAndMembers(t *testing.T) {
	startProjectIntakeTest(t)
	admin := createOrgTestAdmin(t, "proj-candidates-admin@example.com")
	manager := createPermissionTestMember(t, "proj-candidates-manager@example.com")
	unitID := createOrgUnitAs(t, admin, "候选来源部", "")
	project := createOrganizationProject(t, "proj-candidates", unitID)
	joinProject(t, project, manager, "owner")

	rec := httptest.NewRecorder()
	testHandler.ListProjectIntakeCandidates(rec, projectIntakeRequest(manager, project, "GET", "/api/project-intake/candidates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("candidates status = %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Units []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"units"`
		Members []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode candidates: %v %s", err, rec.Body.String())
	}
	var foundUnit bool
	for _, unit := range res.Units {
		if unit.ID == unitID && unit.Path == "候选来源部" {
			foundUnit = true
		}
	}
	if !foundUnit {
		t.Fatalf("candidates units = %+v, want %s", res.Units, unitID)
	}
	var foundManager bool
	for _, member := range res.Members {
		if member.ID == manager {
			foundManager = true
		}
	}
	if !foundManager {
		t.Fatalf("candidates members = %+v, want %s", res.Members, manager)
	}
}

func listRequestableProjects(t *testing.T, userID string) (int, []requestableProjectResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ListRequestableProjects(rec, newRequestAs(userID, "GET", "/api/org/request-projects", nil))
	var res requestableProjectsResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode requestable projects: %v %s", err, rec.Body.String())
		}
	}
	return rec.Code, res.Projects, rec.Body.String()
}

func containsProject(projects []requestableProjectResponse, projectID string) bool {
	for _, project := range projects {
		if project.ID == projectID {
			return true
		}
	}
	return false
}

// TestListRequestableProjects_ScopesToPermission is the C-end picker's only
// source of truth: a project the caller may not hand work is absent.
func TestListRequestableProjects_ScopesToPermission(t *testing.T) {
	startProjectIntakeTest(t)
	admin := createOrgTestAdmin(t, "proj-list-admin@example.com")
	requester := createPermissionTestMember(t, "proj-list-requester@example.com")
	other := createPermissionTestMember(t, "proj-list-other@example.com")
	unitA := createOrgUnitAs(t, admin, "可请求甲部", "")
	unitB := createOrgUnitAs(t, admin, "可请求乙部", "")
	projectA := createOrganizationProject(t, "proj-list-a", unitA)
	projectB := createOrganizationProject(t, "proj-list-b", unitB)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id IN ($1, $2)`, projectA, projectB)
	})

	// The organization's own owner may hand work to every department project.
	status, projects, body := listRequestableProjects(t, admin)
	if status != http.StatusOK {
		t.Fatalf("admin status = %d: %s", status, body)
	}
	if !containsProject(projects, projectA) || !containsProject(projects, projectB) {
		t.Fatalf("admin projects = %+v, want both %s and %s", projects, projectA, projectB)
	}

	// Nobody lists the projects the requester cannot hand work.
	status, projects, body = listRequestableProjects(t, requester)
	if status != http.StatusOK {
		t.Fatalf("requester status = %d: %s", status, body)
	}
	if containsProject(projects, projectA) {
		t.Fatalf("requester projects = %+v, want no %s before any rule allows it", projects, projectA)
	}

	// The owning department's rule is inherited while the project is empty.
	addOrgIntakeSource(t, unitA, "user", requester)
	_, projects, _ = listRequestableProjects(t, requester)
	if !containsProject(projects, projectA) {
		t.Fatalf("requester projects = %+v, want inherited %s", projects, projectA)
	}

	// A project list replaces the department's rule rather than extending it.
	addWorkspaceIntakeSource(t, projectA, "user", other)
	_, projects, _ = listRequestableProjects(t, requester)
	if containsProject(projects, projectA) {
		t.Fatalf("requester projects = %+v, want %s hidden by its own list", projects, projectA)
	}

	// Membership always grants it back.
	joinProject(t, projectA, requester, "member")
	_, projects, _ = listRequestableProjects(t, requester)
	if !containsProject(projects, projectA) {
		t.Fatalf("requester projects = %+v, want %s as a member", projects, projectA)
	}

	// The list answers 409 outside the organization workspace.
	rec := httptest.NewRecorder()
	testHandler.ListRequestableProjects(rec, projectIntakeRequest(requester, projectB, "GET", "/api/org/request-projects", nil))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "dispatch_organization_only") {
		t.Fatalf("project context status = %d, body = %s, want 409 dispatch_organization_only", rec.Code, rec.Body.String())
	}
}

// TestDispatchWork_ProjectTargetUsesItsOwnList: an explicit project_id gates
// the dispatch by the project list (or the owning department when empty), the
// work is delivered into the project workspace, and an unauthorized project is
// indistinguishable from a missing one.
func TestDispatchWork_ProjectTargetUsesItsOwnList(t *testing.T) {
	startDispatchTest(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "proj-dispatch-admin@example.com")
	requester := createPermissionTestMember(t, "proj-dispatch-requester@example.com")
	leader := insertRunnableDutyAgent(t, "proj-dispatch-leader", admin)
	insertRunnableDutyAgent(t, "proj-dispatch-requester-agent", requester)
	unitID := createOrgUnitAs(t, admin, "项目派活部", "")
	placeOrgAgent(t, admin, leader, unitID)
	// The department's duty agent is a company-wide resource: any requester the
	// project admits may call it, without making the requester its owner.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET permission_mode = 'public_to', visibility = 'workspace' WHERE id = $1
	`, leader); err != nil {
		t.Fatalf("make leader invokable: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'workspace', $2)
	`, leader, testWorkspaceID); err != nil {
		t.Fatalf("add leader invocation target: %v", err)
	}
	project := createOrganizationProject(t, "proj-dispatch", unitID)
	projectAgent := insertProjectAgent(t, project, "proj-dispatch-agent", admin)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, project)
		clearDispatchFixtures(t)
	})

	requestSeq := 0
	dispatch := func(extra map[string]any) (int, dispatchWorkResponse, string) {
		requestSeq++
		body := map[string]any{
			"text":              fmt.Sprintf("dispatch-test 项目派活 %d", requestSeq),
			"client_request_id": fmt.Sprintf("33333333-3333-3333-3333-333333333%03d", requestSeq),
		}
		for key, value := range extra {
			body[key] = value
		}
		return postDispatchWork(t, requester, body)
	}

	// The project list admits the requester even though the owning department
	// lists nobody.
	addWorkspaceIntakeSource(t, project, "user", requester)
	status, res, body := dispatch(map[string]any{"project_id": project})
	if status != http.StatusCreated {
		t.Fatalf("project list status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.UnitID != unitID || res.Dispatch.UnitPath != "项目派活部" {
		t.Fatalf("dispatch = %+v, want owning unit %s", res.Dispatch, unitID)
	}
	if res.Issue.WorkspaceID != project {
		t.Fatalf("issue workspace = %s, want project %s", res.Issue.WorkspaceID, project)
	}
	if res.Issue.AssigneeType == nil || *res.Issue.AssigneeType != "agent" || res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != projectAgent {
		t.Fatalf("assignee = %v %v, want project agent %s", res.Issue.AssigneeType, res.Issue.AssigneeID, projectAgent)
	}

	// With no list of its own the project inherits the department, which does
	// not accept the requester.
	testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, project)
	if status, _, body = dispatch(map[string]any{"project_id": project}); status != http.StatusNotFound || !strings.Contains(body, "org_dispatch_unit_not_found") {
		t.Fatalf("inherited denial = %d %s, want 404 org_dispatch_unit_not_found", status, body)
	}

	// The department rule allows the requester, so the empty project does too.
	addOrgIntakeSource(t, unitID, "user", requester)
	if status, _, body = dispatch(map[string]any{"project_id": project}); status != http.StatusCreated {
		t.Fatalf("inherited allow status = %d, want 201: %s", status, body)
	}

	// A project list that omits the requester overrides the department rule.
	addWorkspaceIntakeSource(t, project, "user", admin)
	if status, _, body = dispatch(map[string]any{"project_id": project}); status != http.StatusNotFound {
		t.Fatalf("project override status = %d, want 404: %s", status, body)
	}

	// Target conflicts are rejected before any permission check.
	if status, _, body = dispatch(map[string]any{"project_id": project, "unit_id": unitID}); status != http.StatusBadRequest || !strings.Contains(body, "org_dispatch_target_conflict") {
		t.Fatalf("unit conflict = %d %s, want 400 org_dispatch_target_conflict", status, body)
	}
	if status, _, body = dispatch(map[string]any{"project_id": project, "personal": true}); status != http.StatusBadRequest || !strings.Contains(body, "org_dispatch_target_conflict") {
		t.Fatalf("personal conflict = %d %s, want 400 org_dispatch_target_conflict", status, body)
	}
}

// TestDispatchWork_FreeTextRoutesToNamedProject covers the C-end question
// case: the text names a project under the owning department, the requester
// may hand that project work, no project was selected by hand, and the work
// lands in the project workspace rather than the department queue.
func TestDispatchWork_FreeTextRoutesToNamedProject(t *testing.T) {
	startDispatchTest(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "free-text-route-admin@example.com")
	requester := createPermissionTestMember(t, "free-text-route-requester@example.com")
	leader := insertRunnableDutyAgent(t, "free-text-route-leader", admin)
	insertRunnableDutyAgent(t, "free-text-route-requester-agent", requester)
	unitID := createOrgUnitAs(t, admin, "研发部", "")
	placeOrgAgent(t, admin, leader, unitID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET permission_mode = 'public_to', visibility = 'workspace' WHERE id = $1
	`, leader); err != nil {
		t.Fatalf("make leader invokable: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'workspace', $2)
	`, leader, testWorkspaceID); err != nil {
		t.Fatalf("add leader invocation target: %v", err)
	}
	project := createOrganizationProject(t, "AIHUB", unitID)
	projectAgent := insertProjectAgent(t, project, "free-text-route-project-agent", admin)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, project)
		clearDispatchFixtures(t)
	})
	// The project list admits the requester; the owning department lists nobody,
	// so only the project's own rule can route this request.
	addWorkspaceIntakeSource(t, project, "user", requester)

	status, res, body := postDispatchWork(t, requester, map[string]any{
		"text":              "AIHUB现在接了哪些模型 dispatch-test",
		"client_request_id": "44444444-4444-4444-4444-444444444001",
	})
	if status != http.StatusCreated {
		t.Fatalf("free-text status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.ProjectID != project || res.Dispatch.ProjectName != "AIHUB" {
		t.Fatalf("dispatch project = %q %q, want %s AIHUB", res.Dispatch.ProjectID, res.Dispatch.ProjectName, project)
	}
	if res.Dispatch.UnitID != unitID {
		t.Fatalf("dispatch unit = %s, want owning unit %s", res.Dispatch.UnitID, unitID)
	}
	// The question was asked about AIHUB, so AIHUB runs it: the issue lives in
	// the project workspace and is assigned to its online agent.
	if res.Issue.WorkspaceID != project {
		t.Fatalf("issue workspace = %s, want project %s", res.Issue.WorkspaceID, project)
	}
	if res.Issue.AssigneeType == nil || *res.Issue.AssigneeType != "agent" || res.Issue.AssigneeID == nil || *res.Issue.AssigneeID != projectAgent {
		t.Fatalf("assignee = %v %v, want project agent %s", res.Issue.AssigneeType, res.Issue.AssigneeID, projectAgent)
	}

	status, items, body := listDispatches(t, requester, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", status, body)
	}
	for _, item := range items {
		if item.Issue.ID != res.Issue.ID {
			continue
		}
		if item.ProjectName != "AIHUB" || item.UnitPath != "研发部" {
			t.Fatalf("history row = project %q unit %q, want AIHUB / 研发部", item.ProjectName, item.UnitPath)
		}
		return
	}
	t.Fatalf("dispatch %s missing from history: %s", res.Issue.ID, body)
}

// TestDispatchWork_FreeTextSkipsProjectTheRequesterCannotRequest keeps the
// project list authoritative: naming a project does not route to it when the
// requester is not on its list, and a department-only request still works.
func TestDispatchWork_FreeTextSkipsProjectTheRequesterCannotRequest(t *testing.T) {
	startDispatchTest(t)
	markOrganizationWorkspace(t)
	admin := createOrgTestAdmin(t, "free-text-deny-admin@example.com")
	requester := createPermissionTestMember(t, "free-text-deny-requester@example.com")
	leader := insertRunnableDutyAgent(t, "free-text-deny-leader", admin)
	insertRunnableDutyAgent(t, "free-text-deny-requester-agent", requester)
	unitID := createOrgUnitAs(t, admin, "研发部", "")
	placeOrgAgent(t, admin, leader, unitID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET permission_mode = 'public_to', visibility = 'workspace' WHERE id = $1
	`, leader); err != nil {
		t.Fatalf("make leader invokable: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'workspace', $2)
	`, leader, testWorkspaceID); err != nil {
		t.Fatalf("add leader invocation target: %v", err)
	}
	project := createOrganizationProject(t, "AIHUB", unitID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_intake_source WHERE workspace_id = $1`, project)
		clearDispatchFixtures(t)
	})
	// The department admits the requester; the project's list does not.
	addOrgIntakeSource(t, unitID, "user", requester)
	addWorkspaceIntakeSource(t, project, "user", admin)

	status, res, body := postDispatchWork(t, requester, map[string]any{
		"text":              "AIHUB现在接了哪些模型 dispatch-test-denied",
		"client_request_id": "55555555-5555-5555-5555-555555555001",
	})
	if status != http.StatusCreated {
		t.Fatalf("free-text status = %d, want 201: %s", status, body)
	}
	if res.Dispatch.ProjectID != "" || res.Dispatch.UnitID != unitID {
		t.Fatalf("dispatch = %+v, want owning department without a project", res.Dispatch)
	}

	// With the department also closed, the same text has nowhere to go: the
	// denied project must not be offered as a target.
	testPool.Exec(context.Background(), `
		DELETE FROM org_unit_intake_source WHERE workspace_id = $1 AND org_unit_id = $2
	`, testWorkspaceID, unitID)
	status, _, body = postDispatchWork(t, requester, map[string]any{
		"text":              "AIHUB现在接了哪些模型 dispatch-test-denied 2",
		"client_request_id": "55555555-5555-5555-5555-555555555002",
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "org_dispatch_unroutable") {
		t.Fatalf("closed department = %d %s, want 422 org_dispatch_unroutable", status, body)
	}
}

// TestDeleteWorkspaceAdministration_ClearsProjectIntakeSources keeps the list
// from outliving its project: there is no foreign key to cascade it.
func TestDeleteWorkspaceAdministration_ClearsProjectIntakeSources(t *testing.T) {
	startProjectIntakeTest(t)
	admin := createOrgTestAdmin(t, "proj-delete-admin@example.com")
	unitID := createOrgUnitAs(t, admin, "删除项目部", "")
	project := createOrganizationProject(t, "proj-delete-intake", unitID)
	addWorkspaceIntakeSource(t, project, "user", admin)
	if n := countWorkspaceIntakeSources(t, project); n != 1 {
		t.Fatalf("seeded sources = %d, want 1", n)
	}

	if err := testHandler.Queries.DeleteWorkspaceAdministration(context.Background(), parseUUID(project)); err != nil {
		t.Fatalf("delete workspace administration: %v", err)
	}
	if n := countWorkspaceIntakeSources(t, project); n != 0 {
		t.Fatalf("sources after delete = %d, want 0", n)
	}
}
