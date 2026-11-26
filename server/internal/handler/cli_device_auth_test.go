package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

func withCLIDeviceStore(t *testing.T) auth.CLIDeviceStore {
	t.Helper()
	store := auth.NewMemoryCLIDeviceStore()
	previous := testHandler.CLIDeviceLogins
	testHandler.CLIDeviceLogins = store
	t.Cleanup(func() { testHandler.CLIDeviceLogins = previous })
	return store
}

func testCLIDeviceCode(userCode string) string {
	return userCode + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}

func TestStartCLIDeviceLoginCreatesPendingSession(t *testing.T) {
	store := auth.NewMemoryCLIDeviceStore()
	h := &Handler{CLIDeviceLogins: store}
	req := httptest.NewRequest(http.MethodPost, "/api/cli-device/start", nil)
	var response struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	testutilCall(t, h.StartCLIDeviceLogin, req, http.StatusCreated, &response)

	if response.DeviceCode == "" || !regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}$`).MatchString(response.UserCode) {
		t.Fatalf("invalid response: %+v", response)
	}
	if response.ExpiresIn != 300 || response.Interval < 1 {
		t.Fatalf("invalid timing: %+v", response)
	}
	userCode, digest, ok := parseCLIDeviceCode(response.DeviceCode)
	if !ok || userCode != response.UserCode {
		t.Fatalf("device code does not carry user code: %+v", response)
	}
	if _, err := store.Get(context.Background(), userCode, digest); !errors.Is(err, auth.ErrCLIDevicePending) {
		t.Fatalf("new session error = %v", err)
	}
}

func TestApproveCLIDeviceLogin(t *testing.T) {
	store := auth.NewMemoryCLIDeviceStore()
	h := &Handler{CLIDeviceLogins: store}
	deviceCode := testCLIDeviceCode("ABCD-EFGH")
	if err := store.Create(context.Background(), cliDeviceDigest(deviceCode), "ABCD-EFGH", time.Minute); err != nil {
		t.Fatal(err)
	}

	approve := newRequest(http.MethodPost, "/api/cli-device/approve", map[string]string{
		"user_code": "abcd-efgh",
	})
	testutilCall[map[string]any](t, h.ApproveCLIDeviceLogin, approve, http.StatusOK, nil)
	approval, err := store.Get(context.Background(), "ABCD-EFGH", cliDeviceDigest(deviceCode))
	if err != nil || approval.UserID != testUserID {
		t.Fatalf("approval = %+v, error = %v", approval, err)
	}
}

func TestApproveAndPollCLIDeviceLogin(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}
	store := withCLIDeviceStore(t)
	deviceCode := testCLIDeviceCode("ABCD-EFGH")
	if err := store.Create(context.Background(), cliDeviceDigest(deviceCode), "ABCD-EFGH", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(context.Background(), "ABCD-EFGH", testUserID); err != nil {
		t.Fatal(err)
	}

	poll := newRequest(http.MethodPost, "/api/cli-device/token", map[string]string{
		"device_code": deviceCode,
	})
	var tokenResponse struct {
		Token string `json:"token"`
	}
	testutilCall(t, testHandler.PollCLIDeviceLogin, poll, http.StatusOK, &tokenResponse)
	if tokenResponse.Token == "" {
		t.Fatal("poll did not return a token")
	}

	poll = newRequest(http.MethodPost, "/api/cli-device/token", map[string]string{
		"device_code": deviceCode,
	})
	var repeatedToken struct {
		Token string `json:"token"`
	}
	testutilCall(t, testHandler.PollCLIDeviceLogin, poll, http.StatusOK, &repeatedToken)
	if repeatedToken.Token != tokenResponse.Token {
		t.Fatalf("retry returned a different token")
	}
}

func TestPollCLIDeviceLoginReportsPendingWithoutToken(t *testing.T) {
	store := auth.NewMemoryCLIDeviceStore()
	h := &Handler{CLIDeviceLogins: store}
	deviceCode := testCLIDeviceCode("JKLM-NPQR")
	if err := store.Create(context.Background(), cliDeviceDigest(deviceCode), "JKLM-NPQR", time.Minute); err != nil {
		t.Fatal(err)
	}
	req := newRequest(http.MethodPost, "/api/cli-device/token", map[string]string{
		"device_code": deviceCode,
	})
	rec := httptest.NewRecorder()
	h.PollCLIDeviceLogin(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"] != "authorization_pending" || response["token"] != nil {
		t.Fatalf("response = %#v", response)
	}
}

func testutilCall[T any](
	t *testing.T,
	handler http.HandlerFunc,
	req *http.Request,
	status int,
	out *T,
) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, status, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
}
