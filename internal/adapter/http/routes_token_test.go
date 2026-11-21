package http_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

// A token is accepted on the routes that say so, and on no other. This is the
// list, and these tests are what make it a list: a route does not start
// accepting tokens because somebody forgot to say it does not.
//
// Written before the code, which is why they do not compile yet.

// tokenRoutes is every route that accepts a bearer token, written out.
//
// A ticket that opens a route edits this list in the same change as the table. A test that only checked "the declared routes are
// reasonable" would pass whatever the table said; this one fails the moment a
// route is opened without anybody having decided to.
var tokenRoutes = []string{
	// A pipeline launches a run (#21). The route's own declaration is what makes
	// it safe: the project comes from the scenario, so the authorizer is asked
	// whether this token may run a test in that project, and a token of another
	// project is refused there.
	"POST /runs",
}

func TestExactlyTheDecidedRoutesAcceptATokenForNow(t *testing.T) {
	var got []string
	for _, route := range httpadapter.Routes() {
		if route.AllowToken {
			got = append(got, route.Method+" "+route.Path)
		}
	}
	sort.Strings(got)
	want := append([]string{}, tokenRoutes...)
	sort.Strings(want)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("routes accepting a token:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Whatever route is opened later, it is opened within these limits. A token is
// one project's, holding two permissions: a route that needs more, or that has no
// project, is not one a token can reach.
func TestARouteThatAcceptsATokenIsOneATokenCanSatisfy(t *testing.T) {
	allowed := domain.APITokenPermissions()

	for _, route := range httpadapter.Routes() {
		if !route.AllowToken {
			continue
		}
		key := route.Method + " " + route.Path

		switch route.Scope {
		case httpadapter.ScopePath, httpadapter.ScopeDerived:
		default:
			t.Errorf("%s accepts a token with scope %d: a token has one project, and only a route that names one can check it", key, route.Scope)
		}
		if !allowed.Has(route.Permission) || route.Permission == 0 {
			t.Errorf("%s needs permission %d, which a token does not hold", key, int64(route.Permission))
		}
	}
}

// Nothing that manages an organization, a member, a secret or a token is ever
// open to a token, whatever the table says about the permission. A token that
// could create tokens would be a way to outlive its own revocation.
func TestNoRouteThatManagesAccessAcceptsAToken(t *testing.T) {
	for _, route := range httpadapter.Routes() {
		if !route.AllowToken {
			continue
		}
		for _, forbidden := range []string{"/api-tokens", "/members", "/invitations", "/secrets", "/roles", "/auth", "/me", "/stop-all", "/usage", "/runners"} {
			if strings.Contains(route.Path, forbidden) {
				t.Errorf("%s %s accepts a token, and it is about %s", route.Method, route.Path, forbidden)
			}
		}
	}
}

type countingTokens struct{ calls int }

func (c *countingTokens) Authenticate(context.Context, string) (domain.Identity, error) {
	c.calls++
	return domain.Identity{}, domain.ErrUnauthenticated
}

// The acceptance criterion, stated at the router and driven by the table: on
// every route that did not opt in, a bearer token is a request with no session.
// It is refused with the 401 anybody else gets, and — the part that matters —
// token authentication is never even asked.
func TestEveryRouteThatDidNotOptInRefusesATokenWithoutAskingAboutIt(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions, err := session.NewManager([]byte(strings.Repeat("k", 32)), "https://ballast.test", time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	web, _ := url.Parse("http://web.test")
	ready := func(context.Context) error { return nil }
	tokens := &countingTokens{}

	router := httpadapter.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil,
		auth.NewService(nil, nil, infra.SystemClock{}, log), nil, sessions, ready, web, log,
		httpadapter.WithTokens(tokens))

	const someID = "00000000-0000-4000-8000-000000000001"
	secret := "blt_" + strings.Repeat("A", 43)

	checked := 0
	for _, route := range httpadapter.Routes() {
		if route.Scope == httpadapter.ScopePublic || route.AllowToken {
			continue
		}
		path := strings.NewReplacer(
			"{projectId}", someID, "{userId}", someID, "{id}", someID,
			"{scenarioId}", someID, "{thresholdId}", someID,
			"{environmentId}", someID, "{key}", "API_TOKEN",
		).Replace(route.Path)

		req := httptest.NewRequest(route.Method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+secret)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status %d with a bearer token, want 401", route.Method, route.Path, rec.Code)
		}
		checked++
	}

	if checked == 0 {
		t.Fatal("no route was checked; this test would prove nothing")
	}
	if tokens.calls != 0 {
		t.Errorf("token authentication was asked %d times by routes that did not opt in", tokens.calls)
	}
}

// Managing tokens is for the people who manage the project, and only for them.
// Whatever else changes, these three routes are declared with that permission and
// with the project in the path, so that the middleware enforces the declaration
// instead of leaving it to the service to remember.
func TestTokenManagementIsForProjectAdministrators(t *testing.T) {
	want := map[string]bool{
		"GET /projects/{projectId}/api-tokens":              false,
		"POST /projects/{projectId}/api-tokens":             false,
		"DELETE /projects/{projectId}/api-tokens/{tokenId}": false,
	}

	for _, route := range httpadapter.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; !ok {
			continue
		}
		want[key] = true

		if route.Scope != httpadapter.ScopePath || route.ProjectParam != "projectId" {
			t.Errorf("%s: scope %d, project %q, want the project from the path", key, route.Scope, route.ProjectParam)
		}
		if route.Permission != domain.PermManageProject {
			t.Errorf("%s needs permission %d, want managing the project", key, int64(route.Permission))
		}
		if route.AllowToken {
			t.Errorf("%s accepts a token: a token must never manage tokens", key)
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("%s is not declared", key)
		}
	}
}

// acceptingTokens knows one secret, and counts how often it was asked.
type acceptingTokens struct {
	secret string
	calls  int
}

func (a *acceptingTokens) Authenticate(_ context.Context, secret string) (domain.Identity, error) {
	a.calls++
	if secret != a.secret {
		return domain.Identity{}, domain.ErrUnauthenticated
	}
	return domain.Identity{OrgID: uuid.New(), TokenID: uuid.New()}, nil
}

func routerWithTokens(t *testing.T, tokens *acceptingTokens) http.Handler {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions, err := session.NewManager([]byte(strings.Repeat("k", 32)), "https://ballast.test", time.Hour, infra.SystemClock{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	web, _ := url.Parse("http://web.test")

	return httpadapter.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil,
		auth.NewService(nil, nil, infra.SystemClock{}, log), nil, sessions,
		func(context.Context) error { return nil }, web, log,
		httpadapter.WithTokens(tokens))
}

const aSecret = "blt_" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

// The route that opened to tokens authenticates them and reaches its handler. The
// services are nil here on purpose, so the body is one the handler refuses before
// it would touch one: a 400 about the scenario is the proof that the token was
// accepted, that the request got past authentication, and that nothing below
// was asked.
func TestARouteOpenedToTokensAuthenticatesOneAndReachesItsHandler(t *testing.T) {
	tokens := &acceptingTokens{secret: aSecret}
	router := routerWithTokens(t, tokens)

	request := httptest.NewRequest(http.MethodPost, "/runs", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+aSecret)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "scenario_id is not a uuid") {
		t.Errorf("status %d: %s, want the handler's own 400 about the scenario", recorder.Code, recorder.Body)
	}
	if tokens.calls != 1 {
		t.Errorf("the token was authenticated %d times, want once", tokens.calls)
	}
}

func TestARouteOpenedToTokensRefusesAnotherTokenWith401(t *testing.T) {
	tokens := &acceptingTokens{secret: aSecret}
	router := routerWithTokens(t, tokens)

	request := httptest.NewRequest(http.MethodPost, "/runs", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer blt_"+strings.Repeat("Z", 43))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", recorder.Code)
	}
}

// What the table says about each route is what the spec says too: a route that
// accepts a token documents it, and a route that does not, does not. A spec that
// advertised a way in the router refuses is as wrong as one that hid a way in.
func TestTheSpecDocumentsATokenOnExactlyTheRoutesThatAcceptOne(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean("../../../docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}

	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the spec is not valid YAML: %v", err)
	}

	acceptsToken := map[string]bool{}
	for path, item := range doc.Paths {
		for method, node := range item {
			var op struct {
				Security []map[string][]string `yaml:"security"`
			}
			if err := node.Decode(&op); err != nil {
				continue
			}
			for _, alternative := range op.Security {
				if _, ok := alternative["bearer"]; ok {
					acceptsToken[strings.ToUpper(method)+" "+path] = true
				}
			}
		}
	}

	for _, route := range httpadapter.Routes() {
		key := route.Method + " " + route.Path
		switch {
		case route.AllowToken && !acceptsToken[key]:
			t.Errorf("%s accepts a token and the spec does not say so", key)
		case !route.AllowToken && acceptsToken[key]:
			t.Errorf("the spec says %s accepts a token and the router refuses one", key)
		}
	}
}
