package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestOperatorLogin_UsesBuiltInAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	t.Setenv("MULTICA_OPERATOR_NAME", "")
	t.Setenv("MULTICA_OPERATOR_PASSWORD", "")
	w := httptest.NewRecorder()
	testHandler.OperatorLogin(w, newRequest("POST", "/auth/operator", map[string]any{
		"name": "admin", "password": "admin123456",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", w.Code, w.Body.String())
	}
}

func TestOperatorLogin_RejectsWrongPassword(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	t.Setenv("MULTICA_OPERATOR_NAME", "admin")
	t.Setenv("MULTICA_OPERATOR_PASSWORD", "admin123456")
	w := httptest.NewRecorder()
	testHandler.OperatorLogin(w, newRequest("POST", "/auth/operator", map[string]any{
		"name": "admin", "password": "nope",
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", w.Code, w.Body.String())
	}
}

func TestOperatorLogin_StartsSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	t.Setenv("MULTICA_OPERATOR_NAME", "")
	t.Setenv("MULTICA_OPERATOR_PASSWORD", "")
	w := httptest.NewRecorder()
	testHandler.OperatorLogin(w, newRequest("POST", "/auth/operator", map[string]any{
		"name": "admin", "password": "admin123456",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("expected a session cookie")
	}
}

func TestOperatorSeesWorkspaceWithoutMembership(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID, nil)
	req.Header.Set("X-User-ID", "01999999-9999-7999-8999-999999999999")
	req.Header.Set(auth.OperatorHeader, "1")
	rec := httptest.NewRecorder()
	member, ok := testHandler.requireWorkspaceMember(rec, req, testWorkspaceID, "workspace not found")
	if !ok {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if member.Role != "owner" {
		t.Fatalf("role = %s, want owner", member.Role)
	}
}
