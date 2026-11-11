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
