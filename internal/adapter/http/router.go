package http

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// NewRouter wires the HTTP surface.
//
// net/http's own multiplexer, not a third-party router: since Go 1.22 it
// matches on method and path pattern, which is the whole of what this API
// needs.
func NewRouter(runs *run.Service, orgID uuid.UUID, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	runHandler := handler.NewRun(runs, orgID, log)

	mux.HandleFunc("POST /runs", runHandler.Enqueue)
	mux.HandleFunc("GET /runs/{id}", runHandler.ByID)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return mux
}
