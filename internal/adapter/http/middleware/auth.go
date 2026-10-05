package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
)

// Authenticate lets a request through only with a valid session, and puts who
// sent it in its context. Handlers read the organization from there and pass it
// to the services, which scope every query to it through InTenantTx.
//
// Every refusal is the same 401: telling an expired token from a forged one
// helps nobody but whoever is forging it. The reason goes to the logs.
func Authenticate(sessions *session.Manager, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, err := sessions.Verify(r)
			if err != nil {
				log.DebugContext(r.Context(), "unauthenticated request", "path", r.URL.Path, "reason", err)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(dto.Error{Error: "sign in to continue"})
				return
			}

			next.ServeHTTP(w, r.WithContext(session.WithIdentity(r.Context(), who)))
		})
	}
}
