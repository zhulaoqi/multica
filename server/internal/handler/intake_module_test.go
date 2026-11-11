package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIntakeModule_MemberCannotReplace(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-mod-lead@example.com")
	member := createPermissionTestMember(t, "intake-mod-member@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-mod-agent", member)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/intake/modules", map[string]any{
		"project_id": projectID,
		"modules": []map[string]any{{
			"product": "结算", "module": "导出", "scope": "只导出", "agent_id": agentID,
		}},
	})
	req.Header.Set("X-User-ID", member)
	testHandler.ReplaceIntakeModules(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestIntakeModule_LeadReplacesAndRejectsBadRows(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-mod-lead2@example.com")
	owner := createPermissionTestMember(t, "intake-mod-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	primary := insertIntakeDutyAgent(t, "intake-mod-primary", owner)
	backup := insertIntakeDutyAgent(t, "intake-mod-backup", owner)
	own := insertIntakeDutyAgent(t, "intake-mod-own", lead)

	ownReq := newRequest("PUT", "/api/intake/modules", map[string]any{
		"project_id": projectID,
		"modules": []map[string]any{{
			"product": "结算", "module": "导出", "scope": "只导出", "agent_id": own,
		}},
	})
	ownReq.Header.Set("X-User-ID", lead)
	ownRec := httptest.NewRecorder()
	testHandler.ReplaceIntakeModules(ownRec, ownReq)
	if ownRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("own agent status = %d, want 422: %s", ownRec.Code, ownRec.Body.String())
	}

	same := newRequest("PUT", "/api/intake/modules", map[string]any{
		"project_id": projectID,
		"modules": []map[string]any{{
			"product": "结算", "module": "导出", "scope": "只导出",
			"agent_id": primary, "backup_agent_id": primary,
		}},
	})
	same.Header.Set("X-User-ID", lead)
	sameRec := httptest.NewRecorder()
	testHandler.ReplaceIntakeModules(sameRec, same)
	if sameRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("same backup status = %d, want 422: %s", sameRec.Code, sameRec.Body.String())
	}

	dup := newRequest("PUT", "/api/intake/modules", map[string]any{
		"project_id": projectID,
		"modules": []map[string]any{
			{"product": "结算", "module": "导出", "scope": "只导出", "agent_id": primary},
			{"product": "结算", "module": "导出", "scope": "另一句", "agent_id": backup},
		},
	})
	dup.Header.Set("X-User-ID", lead)
	dupRec := httptest.NewRecorder()
	testHandler.ReplaceIntakeModules(dupRec, dup)
	if dupRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate status = %d, want 422: %s", dupRec.Code, dupRec.Body.String())
	}

	okReq := newRequest("PUT", "/api/intake/modules", map[string]any{
		"project_id": projectID,
		"modules": []map[string]any{{
			"product": "结算", "module": "导出", "scope": "只导出",
			"agent_id": primary, "backup_agent_id": backup,
		}},
	})
	okReq.Header.Set("X-User-ID", lead)
	okRec := httptest.NewRecorder()
	testHandler.ReplaceIntakeModules(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("replace status = %d, want 200: %s", okRec.Code, okRec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM intake_module WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("modules = %d, want 1", count)
	}
}

func insertIntakeProject(t *testing.T, leadID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, lead_type, lead_id)
		VALUES ($1, $2, 'member', $3) RETURNING id
	`, testWorkspaceID, "intake-"+leadID, leadID).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM intake_request WHERE project_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM intake_module WHERE project_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM intake_project_member WHERE project_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, id)
	})
	return id
}

func insertIntakeDutyAgent(t *testing.T, name, ownerID string) string {
	t.Helper()
	agent, err := testHandler.Queries.CreateIntakeAgent(context.Background(), db.CreateIntakeAgentParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Name:        name,
		RuntimeMode: "local",
		OwnerID:     parseUUID(ownerID),
		IntakeCan:   pgtype.Text{String: "负责 " + name, Valid: true},
	})
	if err != nil {
		t.Fatalf("create agent %s: %v", name, err)
	}
	id := uuidToString(agent.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id)
	})
	return id
}
