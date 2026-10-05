package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/middleware"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func setup(t *testing.T) (*session.Manager, http.Handler, *domain.Identity) {
	t.Helper()

	m, err := session.NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour, fixedClock{at: time.Now()})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	var seen domain.Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = session.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return m, middleware.Authenticate(m, log)(next), &seen
}

func TestRequestWithoutSessionIsRefusedWith401(t *testing.T) {
	_, handler, seen := setup(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runs/x", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q, want the API's JSON error", ct)
	}
	if *seen != (domain.Identity{}) {
		t.Error("the protected handler ran without a session")
	}
}

func TestRequestWithInvalidSessionIsRefusedWith401(t *testing.T) {
	_, handler, seen := setup(t)

	r := httptest.NewRequest(http.MethodGet, "/runs/x", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "forged"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
	if *seen != (domain.Identity{}) {
		t.Error("the protected handler ran with a forged session")
	}
}

func TestValidSessionReachesTheHandlerWithItsIdentity(t *testing.T) {
	m, handler, seen := setup(t)
	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}

	issued := httptest.NewRecorder()
	if err := m.Issue(issued, who); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/runs/x", nil)
	for _, c := range issued.Result().Cookies() {
		r.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status %d, want the handler's 204", rec.Code)
	}
	if *seen != who {
		t.Errorf("the handler saw %+v, want %+v", *seen, who)
	}
}
