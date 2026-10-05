package http_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

// The ticket's acceptance criterion, stated at the router: a call without a
// session gets a 401 and never reaches a service. The services are nil here on
// purpose; reaching one would panic and fail the test.
func TestEveryRunRouteRequiresASession(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions, err := session.NewManager([]byte(strings.Repeat("k", 32)), time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	web, _ := url.Parse("http://web.test")

	router := httpadapter.NewRouter(nil, auth.NewService(nil, nil, infra.SystemClock{}, log), sessions, web, log)

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/runs"},
		{http.MethodGet, "/runs/00000000-0000-4000-8000-000000000001"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, strings.NewReader("{}")))

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status %d, want 401", rec.Code)
			}
		})
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("the health check answered %d; it must stay public", rec.Code)
	}
}
