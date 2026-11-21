package middleware_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/middleware"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// A request can prove who it is by a session cookie or by a bearer token, and
// only on the routes that say so. What these tests hold in place:
//
//   - the two never mix: a request that carries an Authorization header is judged
//     by it alone, so a valid cookie cannot rescue a bad token or the reverse;
//   - every refusal is the same 401, and a database that is down is a 503, never
//     a refusal of the credential;
//   - a route that does not accept tokens never even asks.
//
// Written before the code, which is why they do not compile yet.

const goodSecret = "blt_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type fakeTokens struct {
	calls   []string
	who     domain.Identity
	err     error
	secrets map[string]bool
}

func (f *fakeTokens) Authenticate(_ context.Context, secret string) (domain.Identity, error) {
	f.calls = append(f.calls, secret)
	if f.err != nil {
		return domain.Identity{}, f.err
	}
	if !f.secrets[secret] {
		return domain.Identity{}, domain.ErrUnauthenticated
	}
	return f.who, nil
}

type tokenSetup struct {
	manager *session.Manager
	handler http.Handler
	tokens  *fakeTokens
	seen    *domain.Identity
	ran     *bool
}

func newTokenSetup(t *testing.T) tokenSetup {
	t.Helper()

	m, err := session.NewManager([]byte("0123456789abcdef0123456789abcdef"), "https://ballast.test", time.Hour, fixedClock{at: time.Now()})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	tokens := &fakeTokens{
		who:     domain.Identity{OrgID: uuid.New(), TokenID: uuid.New()},
		secrets: map[string]bool{goodSecret: true},
	}

	var seen domain.Identity
	var ran bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		seen, _ = session.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return tokenSetup{
		manager: m,
		handler: middleware.AuthenticateOrToken(m, tokens, log)(next),
		tokens:  tokens,
		seen:    &seen,
		ran:     &ran,
	}
}

func (s tokenSetup) do(t *testing.T, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/runs", nil)
	if mutate != nil {
		mutate(r)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec
}

func withBearer(secret string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+secret) }
}

func TestAValidTokenReachesTheHandlerAsATokenIdentity(t *testing.T) {
	s := newTokenSetup(t)

	rec := s.do(t, withBearer(goodSecret))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want the handler's 204", rec.Code)
	}
	if *s.seen != s.tokens.who || !s.seen.IsToken() {
		t.Errorf("the handler saw %+v, want the token identity %+v", *s.seen, s.tokens.who)
	}
}

func TestTheSchemeIsCaseInsensitive(t *testing.T) {
	s := newTokenSetup(t)

	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		rec := s.do(t, func(r *http.Request) { r.Header.Set("Authorization", scheme+" "+goodSecret) })
		if rec.Code != http.StatusNoContent {
			t.Errorf("scheme %q: status %d, want 204", scheme, rec.Code)
		}
	}
}

func TestAnUnknownTokenIsRefusedWith401AndNeverRuns(t *testing.T) {
	s := newTokenSetup(t)

	rec := s.do(t, withBearer("blt_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q, want the API's JSON error", ct)
	}
	if *s.ran {
		t.Error("the handler ran for a refused token")
	}
}

// A request that carries an Authorization header is judged by it alone. A good
// session cookie next to a bad token is two credentials that disagree, and
// quietly choosing the one that works is how a revoked token keeps working for
// somebody who happens to be signed in.
func TestABadTokenIsNotRescuedByAValidSession(t *testing.T) {
	s := newTokenSetup(t)

	rec := s.do(t, func(r *http.Request) {
		issued := httptest.NewRecorder()
		if err := s.manager.Issue(issued, domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		for _, c := range issued.Result().Cookies() {
			r.AddCookie(c)
		}
		r.Header.Set("Authorization", "Bearer blt_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401: the cookie must not rescue a bad token", rec.Code)
	}
	if *s.ran {
		t.Error("the handler ran on the strength of the cookie")
	}
}

// And the reverse: a valid token is the identity, whatever the cookie says. The
// cookie is not consulted, so a forged one cannot refuse a good token either.
func TestAGoodTokenIgnoresTheCookie(t *testing.T) {
	s := newTokenSetup(t)

	rec := s.do(t, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "forged"})
		r.Header.Set("Authorization", "Bearer "+goodSecret)
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if !s.seen.IsToken() {
		t.Errorf("the handler saw %+v, want the token", *s.seen)
	}
}

// Any Authorization header means the token path. Basic credentials next to a
// valid cookie must not fall through to the cookie: the caller said how it
// wanted to be judged.
func TestAnotherSchemeIsRefusedAndNeverFallsBackToTheSession(t *testing.T) {
	s := newTokenSetup(t)

	for _, header := range []string{"Basic dXNlcjpwYXNz", "Token " + goodSecret, "Bearer", "Bearer ", goodSecret} {
		header := header
		rec := s.do(t, func(r *http.Request) {
			issued := httptest.NewRecorder()
			_ = s.manager.Issue(issued, domain.Identity{UserID: uuid.New(), OrgID: uuid.New()})
			for _, c := range issued.Result().Cookies() {
				r.AddCookie(c)
			}
			r.Header.Set("Authorization", header)
		})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%q: status %d, want 401", header, rec.Code)
		}
	}
	if *s.ran {
		t.Error("the handler ran for a credential that was not a bearer token")
	}
}

func TestTwoAuthorizationHeadersAreRefused(t *testing.T) {
	s := newTokenSetup(t)

	rec := s.do(t, func(r *http.Request) {
		r.Header.Add("Authorization", "Bearer "+goodSecret)
		r.Header.Add("Authorization", "Bearer "+goodSecret)
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401: two credentials are an ambiguity, not a vote", rec.Code)
	}
}

// A token in a URL ends up in access logs, proxies and browser history. It is
// only ever read from the header.
func TestATokenInTheURLIsNeverRead(t *testing.T) {
	s := newTokenSetup(t)

	for _, query := range []string{"token=" + goodSecret, "access_token=" + goodSecret, "api_key=" + goodSecret} {
		rec := s.do(t, func(r *http.Request) { r.URL.RawQuery = query })
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("?%.20s…: status %d, want 401", query, rec.Code)
		}
	}
	if len(s.tokens.calls) != 0 {
		t.Errorf("a token in the URL was handed to authentication: %d calls", len(s.tokens.calls))
	}
}

// Without an Authorization header, nothing changes: the session decides.
func TestWithoutAnAuthorizationHeaderTheSessionDecides(t *testing.T) {
	s := newTokenSetup(t)

	if rec := s.do(t, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no credential: status %d, want 401", rec.Code)
	}

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	rec := s.do(t, func(r *http.Request) {
		issued := httptest.NewRecorder()
		if err := s.manager.Issue(issued, who); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		for _, c := range issued.Result().Cookies() {
			r.AddCookie(c)
		}
	})
	if rec.Code != http.StatusNoContent || *s.seen != who {
		t.Errorf("a valid session: status %d, saw %+v, want 204 and %+v", rec.Code, *s.seen, who)
	}
	if len(s.tokens.calls) != 0 {
		t.Error("a session request consulted token authentication")
	}
}

// A database that is down must not read as a wrong credential: every pipeline
// would be told to rotate a good token in the middle of an outage.
func TestAnOutageIsA503NotA401(t *testing.T) {
	s := newTokenSetup(t)
	s.tokens.err = errors.New("dial tcp 10.0.0.5:5432: connection refused")

	rec := s.do(t, withBearer(goodSecret))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
	// Nothing about the database, and nothing about the token, goes back.
	body := rec.Body.String()
	for _, leak := range []string{"10.0.0.5", "5432", "connection refused", goodSecret} {
		if strings.Contains(body, leak) {
			t.Errorf("the response body leaks %q: %s", leak, body)
		}
	}
	if *s.ran {
		t.Error("the handler ran during an outage")
	}
}

func TestTheSecretIsNeverEchoedBack(t *testing.T) {
	s := newTokenSetup(t)

	for _, secret := range []string{goodSecret, "blt_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"} {
		rec := s.do(t, withBearer(secret))
		dump := rec.Body.String()
		for k, v := range rec.Header() {
			dump += k + ": " + strings.Join(v, ",") + "\n"
		}
		if strings.Contains(dump, secret) {
			t.Errorf("the response repeats the secret: %s", dump)
		}
	}
}

// --- The session-only middleware ------------------------------------------------

// A route that did not opt in refuses a token the way it refuses anything that is
// not a session, and it does so without a lookup: the existing middleware has no
// notion of a token at all, which is what makes the allow-list a list.
func TestSessionOnlyAuthenticationIgnoresABearerToken(t *testing.T) {
	_, handler, seen := setup(t)

	r := httptest.NewRequest(http.MethodGet, "/runs/x", nil)
	r.Header.Set("Authorization", "Bearer "+goodSecret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
	if *seen != (domain.Identity{}) {
		t.Error("the handler ran for a token on a session-only route")
	}
}

// --- The origin guard -------------------------------------------------------------

// The origin check exists because a cookie rides along on a request a page of
// another site makes. A bearer token does not: only code that holds the secret
// can send it, so a request carrying one is not a forged one. It is not asked
// where it came from.
func TestTheOriginGuardLeavesATokenRequestAlone(t *testing.T) {
	r := &reached{}
	h := guarded(r)

	req := httptest.NewRequest(http.MethodPost, "/runs", nil)
	req.Header.Set("Origin", "https://pipeline.example.test")
	req.Header.Set("Authorization", "Bearer "+goodSecret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !r.called {
		t.Errorf("a token request with a foreign Origin: status %d, reached %v, want it let through", rec.Code, r.called)
	}
}

// But only a token request. Anything else carrying a foreign origin is exactly
// what the guard is for, Authorization header or not.
func TestTheOriginGuardStillRefusesWhatIsNotABearerToken(t *testing.T) {
	for _, header := range []string{"", "Basic dXNlcjpwYXNz", "Bearer", "Digest x"} {
		r := &reached{}
		req := httptest.NewRequest(http.MethodPost, "/runs", nil)
		req.Header.Set("Origin", "https://evil.example.test")
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		guarded(r).ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden || r.called {
			t.Errorf("Authorization %q with a foreign Origin: status %d, reached %v, want 403", header, rec.Code, r.called)
		}
	}
}
