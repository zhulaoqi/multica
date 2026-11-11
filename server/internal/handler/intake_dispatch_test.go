package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/llm"
)

func TestIntakeSubmit_PrimaryOnlineEnqueues(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-online-lead@example.com")
	owner := createPermissionTestMember(t, "intake-online-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-online-primary", owner)
	attachOnlineRuntime(t, agentID)
	moduleID := insertPlainModule(t, projectID, agentID, "")
	h := intakeHandlerWithModel(t, moduleID)

	code, raw, issueID := postIntake(t, h, lead, "主责在线就入队")
	if code != http.StatusCreated || containsAll(raw, "enqueue_error") {
		t.Fatalf("create = %d %s", code, raw)
	}
	assignee, queued := intakeAssigneeAndQueue(t, issueID)
	if assignee != agentID || queued < 1 {
		t.Fatalf("assignee = %s queued = %d, want %s and a queue row", assignee, queued, agentID)
	}
	if containsAll(intakeDescription(t, issueID), "主责不在线") {
		t.Fatal("online primary was described as handed to the backup")
	}
}

func TestIntakeSubmit_OfflinePrimaryUsesOnlineBackup(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-backup-lead@example.com")
	owner := createPermissionTestMember(t, "intake-backup-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	primary := insertIntakeDutyAgent(t, "intake-backup-primary", owner)
	backup := insertIntakeDutyAgent(t, "intake-backup-backup", owner)
	attachOnlineRuntime(t, backup)
	moduleID := insertPlainModule(t, projectID, primary, backup)
	h := intakeHandlerWithModel(t, moduleID)

	code, raw, issueID := postIntake(t, h, lead, "主责不在线交给备责")
	if code != http.StatusCreated || containsAll(raw, "enqueue_error") {
		t.Fatalf("create = %d %s", code, raw)
	}
	assignee, queued := intakeAssigneeAndQueue(t, issueID)
	if assignee != backup || queued < 1 {
		t.Fatalf("assignee = %s queued = %d, want backup %s", assignee, queued, backup)
	}
	if !containsAll(intakeDescription(t, issueID), "主责不在线") {
		t.Fatalf("description = %q", intakeDescription(t, issueID))
	}
}

func attachOnlineRuntime(t *testing.T, agentID string) {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'local', 'isolated_test', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, "intake-runtime-"+agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = $1 WHERE id = $2`, runtimeID, agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
}

func insertPlainModule(t *testing.T, projectID, agentID, backupID string) string {
	t.Helper()
	var id string
	var err error
	if backupID == "" {
		err = testPool.QueryRow(context.Background(), `
			INSERT INTO intake_module (workspace_id, project_id, product, module, scope, agent_id, position)
			VALUES ($1, $2, '结算', '在线', '只测在线', $3, 0) RETURNING id
		`, testWorkspaceID, projectID, agentID).Scan(&id)
	} else {
		err = testPool.QueryRow(context.Background(), `
			INSERT INTO intake_module (workspace_id, project_id, product, module, scope, agent_id, backup_agent_id, position)
			VALUES ($1, $2, '结算', '备责', '只测备责', $3, $4, 0) RETURNING id
		`, testWorkspaceID, projectID, agentID, backupID).Scan(&id)
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func intakeHandlerWithModel(t *testing.T, moduleID string) *Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal(map[string]any{
			"module_id": moduleID,
			"tasks":     []string{"先做在线分派"},
		})
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+jsonString(string(payload))+`},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(upstream.Close)
	h := *testHandler
	h.LLM = llm.New(llm.Config{APIKey: "test-key", BaseURL: upstream.URL})
	return &h
}

func postIntake(t *testing.T, h *Handler, userID, text string) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := newRequest("POST", "/api/intake/requests", map[string]any{
		"body": text, "client_request_id": uuid.NewString(),
	})
	req.Header.Set("X-User-ID", userID)
	h.CreateIntakeRequest(rec, req)
	var created struct {
		IssueID string `json:"issue_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	return rec.Code, rec.Body.String(), created.IssueID
}

func intakeAssigneeAndQueue(t *testing.T, issueID string) (string, int) {
	t.Helper()
	var assignee string
	var queued int
	if err := testPool.QueryRow(context.Background(), `
		SELECT assignee_id::text,
		       (SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running'))
		FROM issue WHERE id = $1
	`, issueID).Scan(&assignee, &queued); err != nil {
		t.Fatal(err)
	}
	return assignee, queued
}

func intakeDescription(t *testing.T, issueID string) string {
	t.Helper()
	var description string
	if err := testPool.QueryRow(context.Background(), `SELECT COALESCE(description, '') FROM issue WHERE id = $1`, issueID).Scan(&description); err != nil {
		t.Fatal(err)
	}
	return description
}
