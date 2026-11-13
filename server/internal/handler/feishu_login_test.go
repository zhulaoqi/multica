package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeishuLoginResponse_OmitsPrivateIdentity(t *testing.T) {
	body, err := json.Marshal(feishuLoginResponse{ID: "user-1", Name: "阿宁"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, hidden := range []string{"email", "open_id", "union_id", "avatar"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("response published %s: %s", hidden, text)
		}
	}
}

func TestFeishuBrowserLoginRedirectsWithoutJSON(t *testing.T) {
	rec := &bufferedResponse{header: make(http.Header)}
	rec.Header().Add("Set-Cookie", "multica_session=secret; Path=/")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write([]byte(`{"id":"user-1","name":"Kinch"}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/feishu/callback?code=abc", nil)
	finishFeishuBrowserLogin(w, req, rec)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Fatalf("location = %q, want /", loc)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "multica_session=secret") {
		t.Fatalf("session cookie was not kept: %q", w.Header().Get("Set-Cookie"))
	}
	if strings.Contains(w.Body.String(), "Kinch") {
		t.Fatalf("login JSON was shown to the browser: %s", w.Body.String())
	}
}

func TestFeishuLogin_RequiresCode(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	w := httptest.NewRecorder()
	testHandler.FeishuLogin(w, newRequest("POST", "/auth/feishu", map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
