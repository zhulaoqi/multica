package middleware

import (
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/auth"
)

// RequireOperator gates the platform-level developer console. The header it
// reads is server-set only: Auth deletes any client-supplied X-Operator
// before the branches run and re-stamps it for the developer session alone.
// Mount it after Auth.
func RequireOperator() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(auth.OperatorHeader) != "1" {
				slog.Warn("operator: rejected non-operator request", "path", r.URL.Path, "user_id", r.Header.Get("X-User-ID"))
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
