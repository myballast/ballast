package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fakeAccounts struct{ created []auth.NewAccount }

func (f *fakeAccounts) Lookup(context.Context, string) ([]domain.Identity, error) { return nil, nil }

func (f *fakeAccounts) Me(_ context.Context, who domain.Identity) (auth.Me, error) {
	return auth.Me{UserID: who.UserID, Name: "Ada", Email: "ada@example.test", OrgID: who.OrgID, OrgName: "Ada's organization"}, nil
}

func (f *fakeAccounts) Create(_ context.Context, a auth.NewAccount) error {
	f.created = append(f.created, a)
	return nil
}

type fakeProvider struct {
	err          error
	gotVerifier  string
	authorizeURL string
}

func (f *fakeProvider) AuthCodeURL(state, verifier string) string {
	return f.authorizeURL + "?state=" + url.QueryEscape(state) + "&verifier=" + url.QueryEscape(verifier)
}

func (f *fakeProvider) Exchange(_ context.Context, _, verifier string) (domain.ExternalIdentity, error) {
	f.gotVerifier = verifier
	if f.err != nil {
		return domain.ExternalIdentity{}, f.err
	}
	return domain.ExternalIdentity{Subject: "github|42", Email: "ada@example.test", Name: "Ada"}, nil
}

type rig struct {
	handler  *handler.Auth
	mux      *http.ServeMux
	provider *fakeProvider
	accounts *fakeAccounts
	sessions *session.Manager
}

const webURL = "http://web.test"

func newRig(t *testing.T) rig {
	t.Helper()

	clock := fixedClock{at: time.Now()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := &fakeProvider{authorizeURL: "https://provider.test/authorize"}
	accounts := &fakeAccounts{}

	sessions, err := session.NewManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour, clock)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	web, _ := url.Parse(webURL)

	h := handler.NewAuth(
		auth.NewService(accounts, map[string]auth.IdentityProvider{"github": provider}, clock, log),
		sessions, web, log,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/providers", h.Providers)
	mux.HandleFunc("GET /auth/{provider}/login", h.Login)
	mux.HandleFunc("GET /auth/{provider}/callback", h.Callback)
	mux.HandleFunc("POST /auth/logout", h.Logout)

	return rig{handler: h, mux: mux, provider: provider, accounts: accounts, sessions: sessions}
}

func (r rig) do(method, target string, cookies ...*http.Cookie) *http.Response {
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	return rec.Result()
}

func cookieNamed(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login starts a sign-in and returns the state the provider would echo back
// and the cookie the browser would carry to the callback.
func (r rig) login(t *testing.T, next string) (string, *http.Cookie) {
	t.Helper()

	target := "/auth/github/login"
	if next != "" {
		target += "?next=" + url.QueryEscape(next)
	}
	res := r.do(http.MethodGet, target)
	if res.StatusCode != http.StatusFound {
		t.Fatalf("login answered %d, want a redirect", res.StatusCode)
	}

	location, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}

	flow := cookieNamed(res, handler.FlowCookieName)
	if flow == nil {
		t.Fatal("login set no flow cookie")
	}
	return location.Query().Get("state"), flow
}

func TestLoginSendsTheBrowserToTheProviderWithAFreshState(t *testing.T) {
	r := newRig(t)

	res := r.do(http.MethodGet, "/auth/github/login")
	location := res.Header.Get("Location")
	if !strings.HasPrefix(location, "https://provider.test/authorize?") {
		t.Fatalf("redirected to %q, want the provider", location)
	}

	parsed, _ := url.Parse(location)
	state, verifier := parsed.Query().Get("state"), parsed.Query().Get("verifier")
	if len(state) < 43 || len(verifier) < 43 {
		t.Errorf("state %q and verifier %q must each carry at least 256 bits", state, verifier)
	}

	other, _ := r.login(t, "")
	if other == state {
		t.Error("two sign-ins were given the same state")
	}

	flow := cookieNamed(res, handler.FlowCookieName)
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.SameSite != http.SameSiteLaxMode {
		t.Errorf("the flow cookie must be HttpOnly, Secure and SameSite=Lax: %+v", flow)
	}
	if flow.MaxAge <= 0 || flow.MaxAge > 600 {
		t.Errorf("the flow cookie lives %ds; a sign-in should not take more than ten minutes", flow.MaxAge)
	}
}

func TestLoginThroughAnUnknownProviderIs404(t *testing.T) {
	if res := newRig(t).do(http.MethodGet, "/auth/gitlab/login"); res.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", res.StatusCode)
	}
}

func TestCallbackSignsInAndSetsTheSessionCookie(t *testing.T) {
	r := newRig(t)
	state, flow := r.login(t, "/runs/123?tab=logs")

	res := r.do(http.MethodGet, "/auth/github/callback?code=the-code&state="+url.QueryEscape(state), flow)

	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != webURL+"/runs/123?tab=logs" {
		t.Fatalf("callback answered %d to %q, want a redirect to %s/runs/123?tab=logs", res.StatusCode, res.Header.Get("Location"), webURL)
	}

	c := cookieNamed(res, session.CookieName)
	if c == nil {
		t.Fatal("no session cookie was set")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	who, err := r.sessions.Verify(req)
	if err != nil {
		t.Fatalf("the session cookie does not verify: %v", err)
	}
	if len(r.accounts.created) != 1 || who.OrgID != r.accounts.created[0].OrgID {
		t.Errorf("the session names %+v, want the account just created", who)
	}

	// The verifier went back to the provider: without it, a stolen code is
	// useless, which is what PKCE is for.
	if r.provider.gotVerifier == "" {
		t.Error("the PKCE verifier never reached the provider")
	}

	if cleared := cookieNamed(res, handler.FlowCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Error("the flow cookie outlived the sign-in it was for")
	}
}

// Each of these is a callback the browser did not start, or no longer
// matches the sign-in it did start. None of them may produce a session.
func TestCallbackRefuses(t *testing.T) {
	r := newRig(t)
	state, flow := r.login(t, "")

	cases := map[string]struct {
		target  string
		cookies []*http.Cookie
	}{
		"no flow cookie":     {"/auth/github/callback?code=c&state=" + url.QueryEscape(state), nil},
		"another state":      {"/auth/github/callback?code=c&state=forged", []*http.Cookie{flow}},
		"no state":           {"/auth/github/callback?code=c", []*http.Cookie{flow}},
		"no code":            {"/auth/github/callback?state=" + url.QueryEscape(state), []*http.Cookie{flow}},
		"provider error":     {"/auth/github/callback?error=access_denied&state=" + url.QueryEscape(state), []*http.Cookie{flow}},
		"another provider":   {"/auth/google/callback?code=c&state=" + url.QueryEscape(state), []*http.Cookie{flow}},
		"garbled flow value": {"/auth/github/callback?code=c&state=" + url.QueryEscape(state), []*http.Cookie{{Name: handler.FlowCookieName, Value: "x"}}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := r.do(http.MethodGet, tc.target, tc.cookies...)

			if c := cookieNamed(res, session.CookieName); c != nil && c.Value != "" {
				t.Fatal("a session was issued")
			}
			location := res.Header.Get("Location")
			if res.StatusCode != http.StatusFound || !strings.HasPrefix(location, webURL+"/login?error=") {
				t.Errorf("answered %d to %q, want a redirect to the login page with an error", res.StatusCode, location)
			}
		})
	}
	if len(r.accounts.created) != 0 {
		t.Errorf("%d accounts were created by refused callbacks", len(r.accounts.created))
	}
}

func TestCallbackReportsAFailedExchangeWithoutASession(t *testing.T) {
	r := newRig(t)
	r.provider.err = errors.New("bad_verification_code")
	state, flow := r.login(t, "")

	res := r.do(http.MethodGet, "/auth/github/callback?code=c&state="+url.QueryEscape(state), flow)

	if c := cookieNamed(res, session.CookieName); c != nil && c.Value != "" {
		t.Fatal("a session was issued after a failed exchange")
	}
	if !strings.HasPrefix(res.Header.Get("Location"), webURL+"/login?error=") {
		t.Errorf("redirected to %q, want the login page with an error", res.Header.Get("Location"))
	}
}

// next is only ever a path on the front end. Anything else would make the
// sign-in an open redirect: a link to Ballast that lands somewhere else.
func TestCallbackOnlyRedirectsWithinTheFrontEnd(t *testing.T) {
	for _, next := range []string{
		"https://evil.test/",
		"//evil.test/",
		"/\\evil.test",
		"evil.test",
		"javascript:alert(1)",
	} {
		t.Run(next, func(t *testing.T) {
			r := newRig(t)
			state, flow := r.login(t, next)

			res := r.do(http.MethodGet, "/auth/github/callback?code=c&state="+url.QueryEscape(state), flow)

			if got := res.Header.Get("Location"); got != webURL+"/" {
				t.Errorf("next=%q redirected to %q, want %s/", next, got, webURL)
			}
		})
	}
}

func TestLogoutClearsTheSession(t *testing.T) {
	res := newRig(t).do(http.MethodPost, "/auth/logout")

	if res.StatusCode != http.StatusNoContent {
		t.Errorf("status %d, want 204", res.StatusCode)
	}
	if c := cookieNamed(res, session.CookieName); c == nil || c.MaxAge >= 0 {
		t.Error("logout did not expire the session cookie")
	}
}

func TestProvidersListsWhatIsConfigured(t *testing.T) {
	res := newRig(t).do(http.MethodGet, "/auth/providers")

	var body struct {
		Providers []string `json:"providers"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Providers) != 1 || body.Providers[0] != "github" {
		t.Errorf("providers = %v, want [github]", body.Providers)
	}
}

func TestMeDescribesTheSignedInUser(t *testing.T) {
	r := newRig(t)
	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req = req.WithContext(session.WithIdentity(req.Context(), who))
	rec := httptest.NewRecorder()
	r.handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var body dto.MeResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.User.ID != who.UserID.String() || body.Organization.ID != who.OrgID.String() ||
		body.User.Email != "ada@example.test" || body.Organization.Name != "Ada's organization" {
		t.Errorf("GET /me returned %+v", body)
	}
}

func TestMeWithoutAnIdentityIs401(t *testing.T) {
	rec := httptest.NewRecorder()
	newRig(t).handler.Me(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}
