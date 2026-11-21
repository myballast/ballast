package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// An API token is what a pipeline holds instead of a session. It is a bearer
// credential that ends up in a CI secret store, so what the code below defends is
// what a stolen copy of the database, a log line or a screenshot of the list
// would give away: nothing that works.

// APITokenPrefix starts every secret. A readable prefix lets a secret scanner,
// and a person reading a leaked log, recognise the string for what it is.
const APITokenPrefix = "blt_"

const (
	// apiTokenBytes is how much randomness a secret carries: 256 bits.
	apiTokenBytes = 32
	// apiTokenEncodedLen is those bytes in base64url without padding.
	apiTokenEncodedLen = 43
	// apiTokenDisplayChars is how many characters after the prefix the list
	// shows: 48 bits of the 256, enough to tell two tokens apart and nothing to
	// search with.
	apiTokenDisplayChars = 8
	// apiTokenNameMax is the longest name, in characters.
	apiTokenNameMax = 64
)

const (
	// APITokenDefaultTTL is how long a token lives when its creator does not say.
	APITokenDefaultTTL = 90 * 24 * time.Hour
	// APITokenMaxTTL is the longest a token may live. A credential nobody
	// remembers having issued is the thing to avoid, so there is always an expiry
	// and it is never far.
	APITokenMaxTTL = 365 * 24 * time.Hour
)

// MaxAPITokensPerProject is how many tokens a project may hold that have not been
// revoked. Without a ceiling, creating tokens is a point of unlimited writing,
// and a project with twenty live credentials has lost track of them. A constant,
// like the other ceilings: one that can be raised from outside is one an attacker
// raises.
const MaxAPITokensPerProject = 20

// ErrTooManyAPITokens reports a creation refused because the project already
// holds MaxAPITokensPerProject live tokens. A conflict with what exists, not a
// mistake in the request.
var ErrTooManyAPITokens = errors.New("the project already holds the most API tokens it may")

// ErrInvalidAPIToken reports a token Ballast will not create: a name or an
// expiry that is not acceptable. A mistake in the request, not a refusal.
var ErrInvalidAPIToken = errors.New("invalid API token")

// APITokenState is what became of a token.
type APITokenState string

const (
	// APITokenActive can still be used.
	APITokenActive APITokenState = "active"
	// APITokenRevoked was withdrawn by somebody.
	APITokenRevoked APITokenState = "revoked"
	// APITokenExpired went past its date.
	APITokenExpired APITokenState = "expired"
)

// APIToken is a credential for one project. It holds no secret: only the hash of
// one is ever stored, and the secret is shown once, to whoever created the token.
type APIToken struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	Name       string
	Prefix     string
	CreatedBy  *uuid.UUID
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// State is what became of the token at the moment asked. Revoked outranks
// expired: a token somebody withdrew was withdrawn, whatever its date. The
// instant of expiry is already too late.
func (t APIToken) State(now time.Time) APITokenState {
	switch {
	case t.RevokedAt != nil:
		return APITokenRevoked
	case !now.Before(t.ExpiresAt):
		return APITokenExpired
	}
	return APITokenActive
}

// APITokenPermissions is everything a token may do, in its own project: read it
// and launch a run. Fixed here, and not read from anywhere: a token that could
// hold more would be a way around the roles.
func APITokenPermissions() PermissionSet {
	return PermissionSet(PermViewProject | PermRunTest)
}

// Allows reports whether the token may do perm in projectID at the moment asked.
//
// perm may name several permissions; every one of them has to be held. The
// set's own Has answers "any of them", which for a token would let a request for
// run and manage-the-project be answered by the run alone.
func (t APIToken) Allows(perm Permission, projectID uuid.UUID, now time.Time) bool {
	if perm == 0 || projectID == uuid.Nil || projectID != t.ProjectID {
		return false
	}
	if t.State(now) != APITokenActive {
		return false
	}
	return PermissionSet(perm)&^APITokenPermissions() == 0
}

// NewAPITokenSecret makes a secret from the bytes random gives. A source that
// fails, or gives fewer bytes than asked for, is an error and never a shorter
// token: a predictable credential proves nothing about who holds it.
func NewAPITokenSecret(random io.Reader) (string, error) {
	raw := make([]byte, apiTokenBytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", fmt.Errorf("read random bytes for an API token: %w", err)
	}
	return APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashAPITokenSecret is how a secret is kept: the hex of its SHA-256. The plain
// hash is enough, because the secret already has 256 bits of entropy and there
// is nothing to slow down; and a lookup by hash has to be exact and cheap.
func HashAPITokenSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// APITokenDisplayPrefix is the start of a secret that the list may show, and ""
// for a string that is not a secret.
func APITokenDisplayPrefix(secret string) string {
	if !LooksLikeAPIToken(secret) {
		return ""
	}
	return secret[:len(APITokenPrefix)+apiTokenDisplayChars]
}

// LooksLikeAPIToken reports whether s has the shape of a secret. Whatever arrives
// in an Authorization header is somebody else's input: this keeps garbage, and a
// flood of it, away from the database.
func LooksLikeAPIToken(s string) bool {
	if len(s) != len(APITokenPrefix)+apiTokenEncodedLen || !strings.HasPrefix(s, APITokenPrefix) {
		return false
	}
	for _, c := range s[len(APITokenPrefix):] {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// ValidateAPITokenName checks what a creator wrote and returns the name in the
// form it is kept: trimmed. A control character is refused, for a name is printed
// in lists and in the audit log, and one in it is a forged line in somebody's
// terminal.
func ValidateAPITokenName(name string) (string, error) {
	name = strings.TrimSpace(name)

	if name == "" {
		return "", fmt.Errorf("%w: a token needs a name", ErrInvalidAPIToken)
	}
	if utf8.RuneCountInString(name) > apiTokenNameMax {
		return "", fmt.Errorf("%w: the name is longer than %d characters", ErrInvalidAPIToken, apiTokenNameMax)
	}
	for _, c := range name {
		if unicode.IsControl(c) || c == ' ' || c == ' ' {
			return "", fmt.Errorf("%w: the name holds a control character", ErrInvalidAPIToken)
		}
	}
	return name, nil
}

// ResolveAPITokenExpiry says when a token being created expires. Nothing asked
// for is the default; what was asked for has to be in the future and within the
// ceiling.
func ResolveAPITokenExpiry(requested *time.Time, now time.Time) (time.Time, error) {
	if requested == nil {
		return now.Add(APITokenDefaultTTL), nil
	}
	if !requested.After(now) {
		return time.Time{}, fmt.Errorf("%w: the expiry is not in the future", ErrInvalidAPIToken)
	}
	if requested.After(now.Add(APITokenMaxTTL)) {
		return time.Time{}, fmt.Errorf("%w: a token cannot live longer than a year", ErrInvalidAPIToken)
	}
	return *requested, nil
}

// APITokenCreatedEntry is the journal's line for a token that was created.
//
// The details are a closed set: the prefix, which tells two tokens apart, and the
// expiry. Not the name — free text a person typed, which the journal's rules
// keep out, and which the token's own row holds for whoever follows the entry's
// target. And never the secret, which this function has no way to receive.
func APITokenCreatedEntry(actor *uuid.UUID, token APIToken, at time.Time) AuditEntry {
	return Allow(token.OrgID, actor, ActionAPITokenCreated, TargetAPIToken, at).
		InProject(token.ProjectID).
		On(token.ID).
		With("prefix", token.Prefix).
		With("expires_at", token.ExpiresAt.UTC().Format(time.RFC3339))
}

// APITokenRevokedEntry is the journal's line for a token that was revoked.
func APITokenRevokedEntry(actor *uuid.UUID, token APIToken, at time.Time) AuditEntry {
	return Allow(token.OrgID, actor, ActionAPITokenRevoked, TargetAPIToken, at).
		InProject(token.ProjectID).
		On(token.ID).
		With("prefix", token.Prefix)
}
