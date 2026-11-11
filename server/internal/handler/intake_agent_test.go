package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntakeAgent_EmptyCanDoesNotCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count agents: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CreateIntakeAgent(w, newRequest("POST", "/api/intake/agents", map[string]any{
		"name":         "intake-empty-can",
		"can":          "   ",
		"runtime_mode": "local",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if after != before {
		t.Fatalf("agent count = %d, want %d", after, before)
	}
}

func TestIntakeAgent_RegisterAndListOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ownerID := createPermissionTestMember(t, "intake-agent-owner@example.com")
	otherID := createPermissionTestMember(t, "intake-agent-other@example.com")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/intake/agents", map[string]any{
		"name":         "intake-refund",
		"can":          "退款",
		"cannot":       "登录",
		"runtime_mode": "local",
	})
	req.Header.Set("X-User-ID", ownerID)
	testHandler.CreateIntakeAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Can         string `json:"can"`
		Cannot      string `json:"cannot"`
		RuntimeMode string `json:"runtime_mode"`
		Online      bool   `json:"online"`
		OwnerID     string `json:"owner_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})
	if created.OwnerID != ownerID || created.Can != "退款" || created.Cannot != "登录" || created.Online {
		t.Fatalf("created = %+v", created)
	}

	list := httptest.NewRecorder()
	listReq := newRequest("GET", "/api/intake/agents", nil)
	listReq.Header.Set("X-User-ID", ownerID)
	testHandler.ListIntakeAgents(list, listReq)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
	}
	var rows []struct {
		ID     string `json:"id"`
		Online bool   `json:"online"`
	}
	if err := json.NewDecoder(list.Body).Decode(&rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner list = %+v, missing %s", rows, created.ID)
	}

	other := httptest.NewRecorder()
	otherReq := newRequest("GET", "/api/intake/agents", nil)
	otherReq.Header.Set("X-User-ID", otherID)
	testHandler.ListIntakeAgents(other, otherReq)
	if other.Code != http.StatusOK {
		t.Fatalf("other list status = %d: %s", other.Code, other.Body.String())
	}
	var otherRows []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(other.Body).Decode(&otherRows); err != nil {
		t.Fatalf("decode other: %v", err)
	}
	for _, row := range otherRows {
		if row.ID == created.ID {
			t.Fatalf("other user can see agent %s", created.ID)
		}
	}
}
