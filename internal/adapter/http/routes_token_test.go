package http_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

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
// It is empty on purpose, and the ticket that opens a route edits this list in
// the same change as the table. A test that only checked "the declared routes are
// reasonable" would pass whatever the table said; this one fails the moment a
// route is opened without anybody having decided to.
var tokenRoutes = []string{}

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

	router := httpadapter.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil,
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
