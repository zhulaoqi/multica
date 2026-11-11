package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/llm"
)

func TestIntakeSubmit_NoProjectDoesNotCreateIssue(t *testing.T) {
	requireDB(t)
	member := createPermissionTestMember(t, "intake-submit-none@example.com")
	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1 AND creator_id = $2`, testWorkspaceID, member).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/intake/requests", map[string]any{
		"body": "帮我做导出", "client_request_id": uuid.NewString(),
	})
	req.Header.Set("X-User-ID", member)
	testHandler.CreateIntakeRequest(w, req)
	if w.Code != http.StatusUnprocessableEntity || !containsCode(w.Body.String(), "intake_project_lead_missing") {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1 AND creator_id = $2`, testWorkspaceID, member).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("issues = %d, want %d", after, before)
	}
}

func TestIntakeSubmit_OneIssueAndIdempotentClient(t *testing.T) {
	requireDB(t)
	lead := createPermissionTestMember(t, "intake-submit-lead@example.com")
	owner := createPermissionTestMember(t, "intake-submit-owner@example.com")
	projectID := insertIntakeProject(t, lead)
	agentID := insertIntakeDutyAgent(t, "intake-submit-agent", owner)
	var moduleID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO intake_module (workspace_id, project_id, product, module, scope, agent_id, position)
		VALUES ($1, $2, '结算', '导出', '只做导出', $3, 0) RETURNING id
	`, testWorkspaceID, projectID, agentID).Scan(&moduleID); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal(map[string]any{
			"module_id": moduleID,
			"tasks":     []string{"先接导出", "再接到测试环境"},
		})
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+jsonString(string(payload))+`},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	h := *testHandler
	h.LLM = llm.New(llm.Config{APIKey: "test-key", BaseURL: upstream.URL})

	clientID := uuid.NewString()
	body := map[string]any{"body": "把结算导出接上", "client_request_id": clientID}
	first := httptest.NewRecorder()
	req := newRequest("POST", "/api/intake/requests", body)
	req.Header.Set("X-User-ID", lead)
	h.CreateIntakeRequest(first, req)
	if first.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", first.Code, first.Body.String())
	}
	var created struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	var description, source string
	var issues int
	if err := testPool.QueryRow(context.Background(), `
		SELECT i.description, r.source_body,
		       (SELECT count(*) FROM issue WHERE project_id = $2)
		FROM issue i
		JOIN intake_request r ON r.issue_id = i.id
		WHERE i.id = $1
	`, created.IssueID, projectID).Scan(&description, &source, &issues); err != nil {
		t.Fatal(err)
	}
	if source != "把结算导出接上" {
		t.Fatalf("source = %q", source)
	}
	if !containsAll(description, "先接导出", "再接到测试环境", "在同一个分支上按顺序做完，只部署一次。") {
		t.Fatalf("description = %q", description)
	}
	if issues != 1 {
		t.Fatalf("issues = %d, want 1", issues)
	}

	second := httptest.NewRecorder()
	again := newRequest("POST", "/api/intake/requests", body)
	again.Header.Set("X-User-ID", lead)
	h.CreateIntakeRequest(second, again)
	if second.Code != http.StatusOK || !containsAll(second.Body.String(), created.IssueID) {
		t.Fatalf("repeat status = %d: %s", second.Code, second.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("model calls = %d, want 1", calls.Load())
	}
}

func containsCode(body, code string) bool {
	return strings.Contains(body, code)
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
