// Package session issues and verifies the cookie that says who is signed in.
//
// The cookie holds a JWT with the user and their organization and nothing
// else. Permissions are never in it: they are read from the database on every
// request, so that a revoked role takes effect at once rather than when a token
// happens to expire.
package session

import (
	"context"

	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// CookieName is the session cookie.
const CookieName = "ballast_session"

const issuer = "ballast"

// MinSecretLength is the shortest signing key accepted. HS256 is only as
// strong as its key, and a token is all an attacker needs to brute-force a
// short one offline.
const MinSecretLength = 32

// Clock exists so that expiry can be tested.
type Clock interface {
	Now() time.Time
}

// Manager issues and verifies sessions.
type Manager struct {
	secret []byte
	ttl    time.Duration
	clock  Clock
	parser *jwt.Parser
}

// NewManager returns a manager signing with secret. Sessions last ttl and are
// not renewed: signing in again is the way to extend one.
func NewManager(secret []byte, ttl time.Duration, clock Clock) (*Manager, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("the session secret must be at least %d bytes, got %d", MinSecretLength, len(secret))
	}

	return &Manager{
		secret: secret,
		ttl:    ttl,
		clock:  clock,
		// One algorithm, an issuer, and an expiry that must be present: a
		// token that leaves any of these out is refused rather than trusted
		// by default.
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithTimeFunc(clock.Now),
		),
	}, nil
}

type claims struct {
	Org string `json:"org"`
	jwt.RegisteredClaims
}

// Issue signs a session for who and sets it as a cookie.
func (m *Manager) Issue(w http.ResponseWriter, who domain.Identity) error {
	now := m.clock.Now()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Org: who.OrgID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   who.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}).SignedString(m.secret)
	if err != nil {
		return fmt.Errorf("sign the session: %w", err)
	}

	http.SetCookie(w, cookie(token, int(m.ttl.Seconds())))
	return nil
}

// Clear signs the browser out.
func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, cookie("", -1))
}

// Verify reads the session from a request. Every failure is reported as
// domain.ErrUnauthenticated, with the reason wrapped for the logs only.
func (m *Manager) Verify(r *http.Request) (domain.Identity, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return domain.Identity{}, fmt.Errorf("%w: no session", domain.ErrUnauthenticated)
	}

	var parsed claims
	if _, err := m.parser.ParseWithClaims(c.Value, &parsed, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}); err != nil {
		return domain.Identity{}, fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}

	userID, err := uuid.Parse(parsed.Subject)
	if err != nil || userID == uuid.Nil {
		return domain.Identity{}, fmt.Errorf("%w: the session names no user", domain.ErrUnauthenticated)
	}
	orgID, err := uuid.Parse(parsed.Org)
	if err != nil || orgID == uuid.Nil {
		return domain.Identity{}, fmt.Errorf("%w: the session names no organization", domain.ErrUnauthenticated)
	}

	return domain.Identity{UserID: userID, OrgID: orgID}, nil
}

// cookie is the one place the session cookie's flags are set, so that the
// cookie that clears a session is the same cookie as the one that set it.
//
// Secure holds on http://localhost too: browsers treat it as a secure context.
func cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

type contextKey struct{}

// WithIdentity returns a context carrying who is signed in.
func WithIdentity(ctx context.Context, who domain.Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, who)
}

// FromContext returns who is signed in, if anyone is.
func FromContext(ctx context.Context) (domain.Identity, bool) {
	who, ok := ctx.Value(contextKey{}).(domain.Identity)
	return who, ok
}
