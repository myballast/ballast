package http

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/middleware"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// NewRouter wires the HTTP surface.
//
// net/http's own multiplexer, not a third-party router: since Go 1.22 it
// matches on method and path pattern, which is the whole of what this API
// needs.
//
// Everything is behind a session except the health check and the sign-in flow
// itself. The protected routes are listed below the public ones, each wrapped
// on its own line, so that a route added without it stands out in review.
func NewRouter(
	runs *run.Service,
	signIn *auth.Service,
	sessions *session.Manager,
	web *url.URL,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	runHandler := handler.NewRun(runs, log)
	authHandler := handler.NewAuth(signIn, sessions, web, log)
	protect := middleware.Authenticate(sessions, log)

	// Public.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /auth/providers", authHandler.Providers)
	mux.HandleFunc("GET /auth/{provider}/login", authHandler.Login)
	mux.HandleFunc("GET /auth/{provider}/callback", authHandler.Callback)
	mux.HandleFunc("POST /auth/logout", authHandler.Logout)

	// Signed in.
	mux.Handle("POST /runs", protect(http.HandlerFunc(runHandler.Enqueue)))
	mux.Handle("GET /runs/{id}", protect(http.HandlerFunc(runHandler.ByID)))

	return mux
}
