package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// fakeGitHub plays both GitHub's OAuth2 token endpoint and its REST API.
func fakeGitHub(t *testing.T, user map[string]any, emails []map[string]any) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("code") != "the-code" || r.Form.Get("code_verifier") != "the-verifier" {
			http.Error(w, "bad_verification_code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_test", "token_type": "bearer"})
	})
	authorized := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer gho_test" }
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(user)
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(emails)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func githubAgainst(server *httptest.Server) *GitHub {
	g := NewGitHub("client-id", "client-secret", "http://api.test/auth/github/callback")
	g.conf.Endpoint = oauth2.Endpoint{
		AuthURL:  server.URL + "/login/oauth/authorize",
		TokenURL: server.URL + "/login/oauth/access_token",
	}
	g.apiURL = server.URL
	return g
}

func TestGitHubAuthCodeURLCarriesStateAndPKCEChallenge(t *testing.T) {
	raw := NewGitHub("client-id", "client-secret", "http://api.test/auth/github/callback").AuthCodeURL("the-state", "the-verifier")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if u.Host != "github.com" {
		t.Errorf("sends the browser to %q, want github.com", u.Host)
	}
	if q.Get("state") != "the-state" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("code_challenge") == "the-verifier" {
		t.Errorf("want an S256 challenge derived from the verifier, got %q %q", q.Get("code_challenge_method"), q.Get("code_challenge"))
	}
	if !strings.Contains(q.Get("scope"), "user:email") {
		t.Errorf("scope %q does not ask for the email addresses", q.Get("scope"))
	}
}

func TestGitHubExchangeReadsTheVerifiedPrimaryEmail(t *testing.T) {
	server := fakeGitHub(t,
		map[string]any{"id": 583231, "login": "octocat", "name": "The Octocat"},
		[]map[string]any{
			{"email": "unverified@example.test", "primary": false, "verified": false},
			{"email": "secondary@example.test", "primary": false, "verified": true},
			{"email": "octocat@example.test", "primary": true, "verified": true},
		})

	got, err := githubAgainst(server).Exchange(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if got.Subject != "github|583231" {
		t.Errorf("subject %q, want github|583231: the numeric id, which survives a renamed login", got.Subject)
	}
	if got.Email != "octocat@example.test" || got.Name != "The Octocat" {
		t.Errorf("got %+v", got)
	}
}

func TestGitHubFallsBackToTheLoginWithoutAName(t *testing.T) {
	server := fakeGitHub(t,
		map[string]any{"id": 1, "login": "octocat", "name": nil},
		[]map[string]any{{"email": "octocat@example.test", "primary": true, "verified": true}})

	got, err := githubAgainst(server).Exchange(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.Name != "octocat" {
		t.Errorf("name %q, want the login", got.Name)
	}
}

// An address GitHub has not verified proves nothing about who holds it.
func TestGitHubRefusesAnAccountWithoutAVerifiedEmail(t *testing.T) {
	server := fakeGitHub(t,
		map[string]any{"id": 1, "login": "octocat"},
		[]map[string]any{{"email": "octocat@example.test", "primary": true, "verified": false}})

	if _, err := githubAgainst(server).Exchange(context.Background(), "the-code", "the-verifier"); err == nil {
		t.Error("an account with no verified address was accepted")
	}
}

func TestGitHubRefusesAWrongVerifier(t *testing.T) {
	server := fakeGitHub(t, map[string]any{"id": 1}, nil)

	if _, err := githubAgainst(server).Exchange(context.Background(), "the-code", "another-verifier"); err == nil {
		t.Error("the exchange succeeded with the wrong PKCE verifier")
	}
}
