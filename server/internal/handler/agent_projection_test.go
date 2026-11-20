package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// projectionFixture is an organization (the test workspace) with one owner and
// their primary twin, plus a department project the owner has joined.
type projectionFixture struct {
	owner   string
	source  string
	project string
}

func newProjectionFixture(t *testing.T, slug string) projectionFixture {
	t.Helper()
	requireDB(t)
	ctx := context.Background()
	markOrganizationWorkspace(t)
	owner := createPermissionTestMember(t, slug+"-owner@example.com")
	source := insertIntakeDutyAgent(t, slug+"-twin", owner)

	var unitID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name) VALUES ($1, $2) RETURNING id::text
	`, testWorkspaceID, slug+"-unit").Scan(&unitID); err != nil {
		t.Fatalf("create unit: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE id = $1`, unitID)
	})
	project := createOrganizationProject(t, slug+"-project", unitID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1`, project)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id = $1`, project)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'owner')
	`, project, owner, testUserID); err != nil {
		t.Fatalf("join project: %v", err)
	}
	return projectionFixture{owner: owner, source: source, project: project}
}

type projectionRow struct {
	ID        string
	Name      string
	Can       string
	Primary   bool
	Archived  bool
	RuntimeID string
	Env       string
}

func loadProjection(t *testing.T, project, source string) (projectionRow, bool) {
	t.Helper()
	var row projectionRow
	err := testPool.QueryRow(context.Background(), `
		SELECT id::text, name, COALESCE(intake_can, ''), is_primary, archived_at IS NOT NULL,
		       COALESCE(runtime_id::text, ''), custom_env::text
		FROM agent WHERE workspace_id = $1 AND source_agent_id = $2
	`, project, source).Scan(&row.ID, &row.Name, &row.Can, &row.Primary, &row.Archived, &row.RuntimeID, &row.Env)
	if err != nil {
		return projectionRow{}, false
	}
	return row, true
}

func projectRequest(userID, project, method, path, param, value string, body any) *http.Request {
	req := orgRequestWithParam(userID, method, path, param, value, body)
	req.Header.Set("X-Workspace-ID", project)
	return req
}

func TestEnsurePrimaryTwinInWorkspace_ProjectsOrganizationTwin(t *testing.T) {
	fx := newProjectionFixture(t, "proj-join")
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"TOKEN":"secret"}' WHERE id = $1`, fx.source); err != nil {
		t.Fatalf("seed source env: %v", err)
	}

	for range 2 {
		if err := testHandler.ensurePrimaryTwinInWorkspace(ctx, parseUUID(fx.project), parseUUID(fx.owner)); err != nil {
			t.Fatalf("ensure primary twin: %v", err)
		}
	}

	if count := countRows(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`, fx.project); count != 1 {
		t.Fatalf("project has %d agents, want one projection", count)
	}
	row, ok := loadProjection(t, fx.project, fx.source)
	if !ok {
		t.Fatal("joining the project must project the organization twin")
	}
	if row.ID == fx.source || row.Name != "proj-join-twin" || !row.Primary {
		t.Fatalf("projection = %+v, want a distinct primary row named after the source", row)
	}
	if row.Env != "{}" {
		t.Fatalf("projection env = %s; secrets must stay with the source", row.Env)
	}
}

func TestProjection_FollowsSourceEditsAndArchive(t *testing.T) {
	fx := newProjectionFixture(t, "proj-follow")
	if err := testHandler.ensurePrimaryTwinInWorkspace(context.Background(), parseUUID(fx.project), parseUUID(fx.owner)); err != nil {
		t.Fatalf("ensure primary twin: %v", err)
	}

	rec := httptest.NewRecorder()
	testHandler.PatchIntakeAgent(rec, orgRequestWithAgent(fx.owner, http.MethodPatch, "/api/intake/agents/x", fx.source, map[string]any{
		"name": "proj-follow-renamed", "can": "负责新的范围", "runtime_mode": "local",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch source = %d %s", rec.Code, rec.Body.String())
	}
	row, _ := loadProjection(t, fx.project, fx.source)
	if row.Name != "proj-follow-renamed" || row.Can != "负责新的范围" {
		t.Fatalf("projection = %+v, want the source edit", row)
	}

	rec = httptest.NewRecorder()
	testHandler.ArchiveAgent(rec, orgRequestWithParam(fx.owner, http.MethodPost, "/api/agents/x/archive", "id", fx.source, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive source = %d %s", rec.Code, rec.Body.String())
	}
	if row, _ := loadProjection(t, fx.project, fx.source); !row.Archived || row.Primary {
		t.Fatalf("projection after source archive = %+v, want archived and not primary", row)
	}

	rec = httptest.NewRecorder()
	testHandler.RestoreAgent(rec, orgRequestWithParam(fx.owner, http.MethodPost, "/api/agents/x/restore", "id", fx.source, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore source = %d %s", rec.Code, rec.Body.String())
	}
	if row, _ := loadProjection(t, fx.project, fx.source); row.Archived || !row.Primary {
		t.Fatalf("projection after source restore = %+v, want active primary", row)
	}
}

func TestProjection_IsReadOnlyInProject(t *testing.T) {
	fx := newProjectionFixture(t, "proj-readonly")
	if err := testHandler.ensurePrimaryTwinInWorkspace(context.Background(), parseUUID(fx.project), parseUUID(fx.owner)); err != nil {
		t.Fatalf("ensure primary twin: %v", err)
	}
	row, _ := loadProjection(t, fx.project, fx.source)

	for name, call := range map[string]func(*httptest.ResponseRecorder){
		"update": func(rec *httptest.ResponseRecorder) {
			testHandler.UpdateAgent(rec, projectRequest(fx.owner, fx.project, http.MethodPut, "/api/agents/x", "id", row.ID, map[string]any{"name": "edited here"}))
		},
		"primary": func(rec *httptest.ResponseRecorder) {
			testHandler.SetPrimaryAgent(rec, projectRequest(fx.owner, fx.project, http.MethodPut, "/api/agents/x/primary", "id", row.ID, nil))
		},
		"intake": func(rec *httptest.ResponseRecorder) {
			testHandler.PatchIntakeAgent(rec, projectRequest(fx.owner, fx.project, http.MethodPatch, "/api/intake/agents/x", "agentId", row.ID, map[string]any{
				"name": "edited here", "can": "x", "runtime_mode": "local",
			}))
		},
	} {
		rec := httptest.NewRecorder()
		call(rec)
		if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeAgentManagedByOrganization {
			t.Fatalf("%s projection = %d %s, want 409 %s", name, rec.Code, rec.Body.String(), codeAgentManagedByOrganization)
		}
	}
	if after, _ := loadProjection(t, fx.project, fx.source); after.Name != row.Name {
		t.Fatalf("projection name changed to %q", after.Name)
	}

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, owner_id)
		VALUES ($1, 'proj-readonly-host', 'Claude', 'local', 'claude', 'online', $2)
		RETURNING id::text
	`, fx.project, fx.owner).Scan(&runtimeID); err != nil {
		t.Fatalf("create project runtime: %v", err)
	}
	rec := httptest.NewRecorder()
	req := newRequestAs(fx.owner, http.MethodPost, "/api/agents", map[string]any{
		"name": "project native", "runtime_id": runtimeID,
	})
	req.Header.Set("X-Workspace-ID", fx.project)
	testHandler.CreateAgent(rec, req)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeAgentManagedByOrganization {
		t.Fatalf("create in project = %d %s, want 409 %s", rec.Code, rec.Body.String(), codeAgentManagedByOrganization)
	}
}

func TestCreateIntakeAgent_ProjectsNewPrimaryIntoJoinedProjects(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	markOrganizationWorkspace(t)
	owner := createPermissionTestMember(t, "proj-new-owner@example.com")
	var unitID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name) VALUES ($1, 'proj-new-unit') RETURNING id::text
	`, testWorkspaceID).Scan(&unitID); err != nil {
		t.Fatalf("create unit: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE id = $1`, unitID)
	})
	project := createOrganizationProject(t, "proj-new-project", unitID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE owner_id = $1`, owner)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, project, owner); err != nil {
		t.Fatalf("join project: %v", err)
	}

	rec := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(rec, newRequestAs(owner, http.MethodPost, "/api/intake/agents", map[string]any{
		"name": "proj-new-twin", "can": "负责新人", "runtime_mode": "local",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create organization agent = %d %s", rec.Code, rec.Body.String())
	}
	var created intakeAgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if row, ok := loadProjection(t, project, created.ID); !ok || !row.Primary {
		t.Fatalf("projection = %+v (found %v), want the new primary projected into the project", row, ok)
	}

	rec = httptest.NewRecorder()
	req := newRequestAs(owner, http.MethodPost, "/api/intake/agents", map[string]any{
		"name": "proj-new-native", "can": "x", "runtime_mode": "local",
	})
	req.Header.Set("X-Workspace-ID", project)
	testHandler.CreateIntakeAgent(rec, req)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != codeAgentManagedByOrganization {
		t.Fatalf("intake create in project = %d %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestSyncProjectionsForDaemon_MapsProjectRuntime(t *testing.T) {
	fx := newProjectionFixture(t, "proj-daemon")
	ctx := context.Background()
	const daemonID = "proj-daemon-host"
	insertRuntime := func(workspaceID string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, owner_id, last_seen_at)
			VALUES ($1, $2, 'Claude', 'local', 'claude', 'online', $3, now())
			RETURNING id::text
		`, workspaceID, daemonID, fx.owner).Scan(&id); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, id)
		})
		return id
	}
	sourceRuntime := insertRuntime(testWorkspaceID)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = $2 WHERE id = $1`, fx.source, sourceRuntime); err != nil {
		t.Fatalf("bind source runtime: %v", err)
	}

	if err := testHandler.ensurePrimaryTwinInWorkspace(ctx, parseUUID(fx.project), parseUUID(fx.owner)); err != nil {
		t.Fatalf("ensure primary twin: %v", err)
	}
	if row, _ := loadProjection(t, fx.project, fx.source); row.RuntimeID != "" {
		t.Fatalf("projection runtime = %q before the daemon joined the project, want none", row.RuntimeID)
	}

	projectRuntime := insertRuntime(fx.project)
	testHandler.syncProjectionsForDaemon(ctx, parseUUID(fx.project), daemonID)
	if row, _ := loadProjection(t, fx.project, fx.source); row.RuntimeID != projectRuntime {
		t.Fatalf("projection runtime = %q, want the project runtime %q", row.RuntimeID, projectRuntime)
	}
}

func TestCreateAgentProjections_AddsMembersAgents(t *testing.T) {
	fx := newProjectionFixture(t, "proj-add")
	extra := insertIntakeDutyAgent(t, "proj-add-helper", fx.owner)
	outsider := createPermissionTestMember(t, "proj-add-outsider@example.com")
	foreign := insertIntakeDutyAgent(t, "proj-add-foreign", outsider)

	post := func(ids ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/agents/projections", map[string]any{"agent_ids": ids})
		req.Header.Set("X-Workspace-ID", fx.project)
		testHandler.CreateAgentProjections(rec, req)
		return rec
	}

	if rec := post(foreign); rec.Code != http.StatusNotFound {
		t.Fatalf("agent of a non-member = %d %s, want 404", rec.Code, rec.Body.String())
	}
	rec := post(extra)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add projection = %d %s", rec.Code, rec.Body.String())
	}
	var out []AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 1 {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if out[0].SourceAgentID == nil || *out[0].SourceAgentID != extra || out[0].IsPrimary {
		t.Fatalf("projection response = %+v, want a non-primary projection of %s", out[0], extra)
	}

	member := httptest.NewRecorder()
	req := newRequestAs(fx.owner, http.MethodPost, "/api/agents/projections", map[string]any{"agent_ids": []string{extra}})
	req.Header.Set("X-Workspace-ID", fx.project)
	testHandler.CreateAgentProjections(member, req)
	if member.Code != http.StatusForbidden {
		t.Fatalf("plain member add = %d, want 403", member.Code)
	}
}
