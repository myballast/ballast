package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/http/middleware"
)

const (
	front = "https://front.example.test"
	api   = "https://api.example.test"
)

// reached records whether the request got past the guard.
type reached struct{ called bool }

func (r *reached) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	r.called = true
	w.WriteHeader(http.StatusOK)
}

func guarded(next http.Handler) http.Handler {
	return middleware.OriginGuard([]string{front, api}, slog.New(slog.NewTextHandler(io.Discard, nil)))(next)
}

func do(h http.Handler, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/projects", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The front end calls the API from its server, and curl from anywhere: neither
// sends an Origin, and neither is a browser page of somebody else's. Whether
// they may do anything is the session's business.
func TestARequestWithNoOriginIsLeftToAuthentication(t *testing.T) {
	next := &reached{}

	rec := do(guarded(next), http.MethodGet, "")

	if !next.called || rec.Code != http.StatusOK {
		t.Errorf("a request with no Origin was stopped: %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS headers were set on a request that is not cross-origin")
	}
}

func TestTheOriginOfTheFrontEndIsAllowedAndAnswered(t *testing.T) {
	next := &reached{}

	rec := do(guarded(next), http.MethodPost, front)

	if !next.called {
		t.Fatalf("the front end's own origin was refused: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != front {
		t.Errorf("Access-Control-Allow-Origin = %q, want the origin that asked", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials are not allowed for the front end")
	}
	// Without Vary, a cache hands one origin's answer to another.
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
	}
}

func TestAnyOtherOriginIsRefusedBeforeTheHandlerRuns(t *testing.T) {
	for name, origin := range map[string]string{
		"another site":            "https://evil.example",
		"the null origin":         "null",
		"a lookalike suffix":      "https://front.example.test.evil.example",
		"a lookalike prefix":      "https://evilfront.example.test",
		"the same host, a port":   "https://front.example.test:444",
		"the same host, in clear": "http://front.example.test",
		"a path":                  "https://front.example.test/x",
		"nothing but a scheme":    "https://",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(name+" "+method, func(t *testing.T) {
				next := &reached{}

				rec := do(guarded(next), method, origin)

				if next.called {
					t.Fatalf("a request from %q reached the handler", origin)
				}
				if rec.Code != http.StatusForbidden {
					t.Errorf("status = %d, want 403", rec.Code)
				}
				if rec.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Error("a refused origin was told it may read the answer")
				}
				// What the caller sent is not repeated: it is theirs, and it ends
				// up in a page.
				if body, _ := io.ReadAll(rec.Body); len(body) > 0 && contains(string(body), origin) {
					t.Errorf("the refusal repeats the origin: %s", body)
				}
			})
		}
	}
}

func TestAPreflightFromTheFrontEndIsAnsweredWithoutReachingTheHandler(t *testing.T) {
	next := &reached{}

	rec := do(guarded(next), http.MethodOptions, front)

	if next.called {
		t.Error("a preflight reached the handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != front {
		t.Error("the preflight was not answered for the front end")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("the preflight says nothing about methods or headers")
	}
}

func TestAPreflightFromAnotherOriginIsRefused(t *testing.T) {
	rec := do(guarded(&reached{}), http.MethodOptions, "https://evil.example")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestOriginOfKeepsTheSchemeTheHostAndThePortOnly(t *testing.T) {
	for in, want := range map[string]string{
		"https://front.example.test/login?next=/x": "https://front.example.test",
		"https://Front.Example.test":               "https://front.example.test",
		"http://localhost:3000/":                   "http://localhost:3000",
		"https://front.example.test:443/":          "https://front.example.test",
		"http://front.example.test:80":             "http://front.example.test",
		"https://front.example.test:8443":          "https://front.example.test:8443",
	} {
		if got := middleware.OriginOf(mustParse(t, in)); got != want {
			t.Errorf("OriginOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
