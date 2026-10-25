package session_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// audience is the origin of the environment the managers below belong to.
const audience = "https://staging.example.test"

var (
	secret = []byte("0123456789abcdef0123456789abcdef")
	now    = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	who    = domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func manager(t *testing.T, at time.Time) *session.Manager {
	t.Helper()

	m, err := session.NewManager(secret, audience, 12*time.Hour, fixedClock{at: at})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// issue returns the cookie a successful sign-in sets.
func issue(t *testing.T, m *session.Manager) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := m.Issue(rec, who); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Issue set %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func requestWith(c *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/runs/x", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestSessionRoundTrips(t *testing.T) {
	m := manager(t, now)

	got, err := m.Verify(requestWith(issue(t, m)))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != who {
		t.Errorf("Verify returned %+v, want %+v", got, who)
	}
}

// The cookie is out of reach of page scripts, never sent in clear, and not
// sent on cross-site subrequests — which is what stands between a session and
// a forged POST /runs from somebody else's page.
func TestSessionCookieFlags(t *testing.T) {
	c := issue(t, manager(t, now))

	if !c.HttpOnly {
		t.Error("the session cookie is readable by scripts")
	}
	if !c.Secure {
		t.Error("the session cookie may travel over plain HTTP")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite is %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path is %q, want /", c.Path)
	}
	if c.MaxAge != int((12 * time.Hour).Seconds()) {
		t.Errorf("MaxAge is %d, want the session's lifetime", c.MaxAge)
	}
}

// The token says who and for which organization. Nothing about what they may
// do: permissions are read from the database on every request.
func TestTokenCarriesNoPermission(t *testing.T) {
	c := issue(t, manager(t, now))

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(c.Value, claims); err != nil {
		t.Fatalf("parse: %v", err)
	}

	allowed := map[string]bool{"iss": true, "sub": true, "aud": true, "org": true, "iat": true, "exp": true}
	for name := range claims {
		if !allowed[name] {
			t.Errorf("the token carries an unexpected claim %q", name)
		}
	}
}

func TestVerifyRefuses(t *testing.T) {
	m := manager(t, now)
	valid := issue(t, m)

	forged := func(method jwt.SigningMethod, key any, claims jwt.MapClaims) *http.Cookie {
		token, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return &http.Cookie{Name: valid.Name, Value: token}
	}
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": "ballast",
			"aud": audience,
			"sub": who.UserID.String(),
			"org": who.OrgID.String(),
			"iat": now.Unix(),
			"exp": now.Add(time.Hour).Unix(),
		}
	}
	without := func(name string) jwt.MapClaims {
		c := claims()
		delete(c, name)
		return c
	}
	with := func(name string, value any) jwt.MapClaims {
		c := claims()
		c[name] = value
		return c
	}

	cases := map[string]struct {
		cookie *http.Cookie
		m      *session.Manager
	}{
		"no cookie":        {nil, m},
		"empty cookie":     {&http.Cookie{Name: valid.Name, Value: ""}, m},
		"garbage":          {&http.Cookie{Name: valid.Name, Value: "not.a.token"}, m},
		"expired":          {valid, manager(t, now.Add(13*time.Hour))},
		"another key":      {forged(jwt.SigningMethodHS256, []byte("another-secret-another-secret-xx"), claims()), m},
		"alg none":         {forged(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims()), m},
		"another HMAC alg": {forged(jwt.SigningMethodHS512, secret, claims()), m},
		"no expiry":        {forged(jwt.SigningMethodHS256, secret, without("exp")), m},
		"another issuer":   {forged(jwt.SigningMethodHS256, secret, with("iss", "someone-else")), m},
		"no audience":      {forged(jwt.SigningMethodHS256, secret, without("aud")), m},
		"another audience": {forged(jwt.SigningMethodHS256, secret, with("aud", "https://production.example.test")), m},
		"no organization":  {forged(jwt.SigningMethodHS256, secret, without("org")), m},
		"bad organization": {forged(jwt.SigningMethodHS256, secret, with("org", "not-a-uuid")), m},
		"nil organization": {forged(jwt.SigningMethodHS256, secret, with("org", uuid.Nil.String())), m},
		"bad subject":      {forged(jwt.SigningMethodHS256, secret, with("sub", "not-a-uuid")), m},
		"tampered":         {&http.Cookie{Name: valid.Name, Value: valid.Value[:len(valid.Value)-2] + "xx"}, m},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.m.Verify(requestWith(tc.cookie))
			if !errors.Is(err, domain.ErrUnauthenticated) {
				t.Errorf("Verify returned %v, want ErrUnauthenticated", err)
			}
		})
	}
}

// A short key can be brute-forced offline from any one token.
func TestManagerRefusesAShortSecret(t *testing.T) {
	if _, err := session.NewManager([]byte("too-short"), audience, time.Hour, fixedClock{at: now}); err == nil {
		t.Error("a 9-byte secret was accepted")
	}
}

func TestClearExpiresTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	manager(t, now).Clear(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Value != "" {
		t.Fatalf("Clear set %+v, want one expired, empty cookie", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Error("the clearing cookie must carry the same flags as the session")
	}
}

func TestIdentityTravelsThroughTheContext(t *testing.T) {
	r := requestWith(nil)
	if _, ok := session.FromContext(r.Context()); ok {
		t.Fatal("an empty context yielded an identity")
	}

	got, ok := session.FromContext(session.WithIdentity(r.Context(), who))
	if !ok || got != who {
		t.Errorf("FromContext returned %+v, %v", got, ok)
	}
}

// A cookie stolen from one environment opens nothing in another. The secrets
// differ between environments, and that alone is enough; this is the second
// lock, for the day two of them are given the same one by mistake.
func TestASessionOfAnotherEnvironmentIsRefusedEvenWithTheSameSecret(t *testing.T) {
	staging := manager(t, now)
	production, err := session.NewManager(secret, "https://production.example.test", 12*time.Hour, fixedClock{at: now})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	stolen := issue(t, staging)

	if _, err := production.Verify(requestWith(stolen)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a staging session opened production: %v", err)
	}
	if _, err := staging.Verify(requestWith(stolen)); err != nil {
		t.Errorf("a staging session no longer opens staging: %v", err)
	}
}

// An audience compared loosely is an audience that matches a neighbour.
func TestTheAudienceIsAnOriginNotAPrefix(t *testing.T) {
	staging := manager(t, now)
	lookalike, err := session.NewManager(secret, audience+".evil.test", 12*time.Hour, fixedClock{at: now})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, err := lookalike.Verify(requestWith(issue(t, staging))); err == nil {
		t.Error("a session was accepted by an environment whose origin only starts the same")
	}
}

func TestManagerRefusesNoAudience(t *testing.T) {
	if _, err := session.NewManager(secret, "", time.Hour, fixedClock{at: now}); err == nil {
		t.Error("a manager with no audience was accepted: its sessions would open every environment")
	}
}
