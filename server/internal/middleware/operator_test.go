package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestRequireOperatorRejectsMissingHeader(t *testing.T) {
	handler := RequireOperator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not be called")
	}))

	req := httptest.NewRequest("GET", "/api/operator/overview", nil)
	req.Header.Set("X-User-ID", "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if body := w.Body.String(); body != `{"error":"insufficient permissions"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestRequireOperatorRejectsOtherHeaderValues(t *testing.T) {
	handler := RequireOperator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not be called")
	}))

	req := httptest.NewRequest("GET", "/api/operator/overview", nil)
	req.Header.Set(auth.OperatorHeader, "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequireOperatorAllowsStampedSession(t *testing.T) {
	handler := RequireOperator()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/operator/overview", nil)
	req.Header.Set(auth.OperatorHeader, "1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
