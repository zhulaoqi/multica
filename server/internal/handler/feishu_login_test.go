package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingFeishuExchanger struct{}

func (failingFeishuExchanger) Exchange(context.Context, string) (FeishuProfile, error) {
	return FeishuProfile{}, errors.New("feishu token exchange failed: code=20003")
}

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

func TestFeishuLoginCallback_ReturnsToLoginWhenExchangeFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not available")
	}
	previous := testHandler.FeishuLoginExchanger
	testHandler.FeishuLoginExchanger = failingFeishuExchanger{}
	t.Cleanup(func() { testHandler.FeishuLoginExchanger = previous })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/feishu/callback?code=stale&state=intake", nil)
	testHandler.FeishuLoginCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/login?error=feishu" {
		t.Fatalf("Location = %q, want /login?error=feishu", got)
	}
	if body := w.Body.String(); strings.Contains(body, "feishu login failed") {
		t.Fatalf("error body leaked to the browser: %s", body)
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
