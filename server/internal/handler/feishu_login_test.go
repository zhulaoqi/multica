package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type failingFeishuExchanger struct{}

func (failingFeishuExchanger) Exchange(context.Context, string) (FeishuProfile, error) {
	return FeishuProfile{}, errors.New("feishu token exchange failed: code=20003")
}

type profileFeishuExchanger struct{ profile FeishuProfile }

func (e profileFeishuExchanger) Exchange(context.Context, string) (FeishuProfile, error) {
	return e.profile, nil
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
	// A 302 must not carry the JSON body's framing headers: the web proxy
	// drops the connection when Content-Length promises a body that never
	// arrives, and the browser sees an empty reply instead of the redirect.
	if got := w.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q on a redirect with no body", got)
	}
	if got := w.Header().Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type = %q on a redirect with no body", got)
	}
}

func TestFeishuLoginCallback_SetsSessionCookiesAndSkipsTheLoginPage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler not available")
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	email := "feishu-callback-" + suffix + "@multica.test"
	openID := "ou_callback_" + suffix
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM feishu_identity WHERE open_id = $1`, openID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	previous := testHandler.FeishuLoginExchanger
	testHandler.FeishuLoginExchanger = profileFeishuExchanger{profile: FeishuProfile{
		OpenID: openID, Name: "飞书回调", Email: email,
	}}
	t.Cleanup(func() { testHandler.FeishuLoginExchanger = previous })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/feishu/callback?code=fresh&state=intake", nil)
	testHandler.FeishuLoginCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", w.Code, w.Body.String())
	}
	// The visitor is brand new, so the landing is the onboarding flow rather
	// than the site root that would bounce back through /login.
	if got := w.Header().Get("Location"); got != "/onboarding" {
		t.Fatalf("Location = %q, want /onboarding", got)
	}
	if body := w.Body.String(); strings.Contains(body, "飞书回调") {
		t.Fatalf("login JSON was shown to the browser: %s", body)
	}
	var sessionCookie bool
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "multica_auth" {
			sessionCookie = true
		}
	}
	if !sessionCookie {
		t.Fatalf("redirect did not carry the session cookie: %v", w.Result().Cookies())
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
