package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// fakeGoogle plays Google's token endpoint and its signing keys. Each test
// decides what the ID token in the token response says, and with which key it
// is signed.
type fakeGoogle struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	// idToken is what the next token response carries.
	idToken string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	f := &fakeGoogle{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /certs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("code_verifier") != "the-verifier" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ya29.test", "token_type": "Bearer", "expires_in": 3600, "id_token": f.idToken,
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) sign(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func (f *fakeGoogle) claims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            f.server.URL,
		"aud":            "client-id",
		"sub":            "110169484474386276334",
		"email":          "ada@example.test",
		"email_verified": true,
		"name":           "Ada Lovelace",
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
}

func (f *fakeGoogle) provider() *Google {
	g := newGoogle("client-id", "client-secret", "http://api.test/auth/google/callback", f.server.URL, f.server.URL+"/certs")
	g.conf.Endpoint = oauth2.Endpoint{AuthURL: f.server.URL + "/auth", TokenURL: f.server.URL + "/token"}
	return g
}

func TestGoogleExchangeVerifiesTheIDToken(t *testing.T) {
	f := newFakeGoogle(t)
	f.idToken = f.sign(t, f.key, f.claims())

	got, err := f.provider().Exchange(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.Subject != "google|110169484474386276334" || got.Email != "ada@example.test" || got.Name != "Ada Lovelace" {
		t.Errorf("got %+v", got)
	}
}

func TestGoogleRefuses(t *testing.T) {
	f := newFakeGoogle(t)
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	with := func(name string, value any) jwt.MapClaims {
		c := f.claims()
		c[name] = value
		return c
	}

	cases := map[string]string{
		"another key":        f.sign(t, stranger, f.claims()),
		"another audience":   f.sign(t, f.key, with("aud", "somebody-elses-app")),
		"another issuer":     f.sign(t, f.key, with("iss", "https://evil.test")),
		"expired":            f.sign(t, f.key, with("exp", time.Now().Add(-time.Hour).Unix())),
		"unverified email":   f.sign(t, f.key, with("email_verified", false)),
		"no email":           f.sign(t, f.key, with("email", "")),
		"no id token at all": "",
	}

	for name, idToken := range cases {
		t.Run(name, func(t *testing.T) {
			f.idToken = idToken
			if got, err := f.provider().Exchange(context.Background(), "the-code", "the-verifier"); err == nil {
				t.Errorf("accepted as %+v", got)
			}
		})
	}
}

func TestGoogleAuthCodeURLAsksForOpenIDWithPKCE(t *testing.T) {
	raw := NewGoogle("client-id", "client-secret", "http://api.test/auth/google/callback").AuthCodeURL("the-state", "the-verifier")

	if want := "https://accounts.google.com/"; len(raw) < len(want) || raw[:len(want)] != want {
		t.Errorf("sends the browser to %q", raw)
	}
	for _, part := range []string{"scope=openid+email+profile", "state=the-state", "code_challenge_method=S256"} {
		if !strings.Contains(raw, part) {
			t.Errorf("%q is missing %q", raw, part)
		}
	}
}
