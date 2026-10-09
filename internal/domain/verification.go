package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The challenge is a file served at the root of the target's origin. RFC 8615
// reserves /.well-known for exactly this, and puts it at the root — so a base
// URL carrying a path does not drag that path into the challenge.
const (
	wellKnownPath = "/.well-known/"
	proofPrefix   = "ballast-"
)

// proofToken is the shape a challenge token may have: the base64url alphabet and
// nothing else.
//
// The token is interpolated into a URL path. One carrying a slash, a dot-dot or
// an encoded slash would make Ballast read a different path than the one the
// user was told to create — or climb out of /.well-known altogether. The
// generator produces base64url, and this is what keeps the two facts tied
// together rather than merely adjacent.
var proofToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// RequireProvable refuses an attempt to verify a target that is already
// approved.
//
// Recording a proof moves a target to proof_ok. Run against an approved one, it
// would quietly take the approval away — an endpoint that reduces permissions
// while looking like it confirms them. Pointing the environment somewhere else
// is what resets it, and that is the edit path.
func (e Environment) RequireProvable() error {
	if e.VerificationStatus == VerificationVerified {
		return fmt.Errorf("%w: edit the target if you mean to point it elsewhere", ErrAlreadyVerified)
	}
	return nil
}

// RequireApprovable refuses an approval of a target whose proof is not in.
//
// This is what "distinct" means in the two-step verification: an administrator
// approves a proof somebody else's server actually served. Approving without one
// would make the whole challenge decorative.
func (e Environment) RequireApprovable() error {
	switch e.VerificationStatus {
	case VerificationVerified:
		return fmt.Errorf("%w: it was approved already", ErrAlreadyVerified)
	case VerificationProofOK:
		return nil
	default:
		return fmt.Errorf("%w: its status is %q", ErrNotProven, e.VerificationStatus)
	}
}

// ProofURL is where the challenge has to be served.
//
// Built from the origin alone — scheme, host and port — because /.well-known
// lives at the root. The token is checked rather than trusted: this function
// builds a URL that Ballast will then fetch, and it is handed to it from the
// database.
func (e Environment) ProofURL() (string, error) {
	origin, err := e.origin()
	if err != nil {
		return "", err
	}

	if !proofToken.MatchString(e.VerificationToken) {
		// The token itself is not repeated: it goes nowhere near a log.
		return "", fmt.Errorf("%w: the verification token is not a usable path segment", ErrInvalidEnvironment)
	}

	return origin + wellKnownPath + proofPrefix + e.VerificationToken, nil
}

// Host is the hostname an approval adds to the project's allowlist.
//
// The hostname alone, lower case, without the port: a port is not a host, and
// what the allowlist bounds is where traffic may go.
func (e Environment) Host() (string, error) {
	parsed, err := url.Parse(e.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: the base URL names no host", ErrInvalidEnvironment)
	}

	return strings.ToLower(parsed.Hostname()), nil
}

// origin is the part of the base URL a proof of ownership covers.
func (e Environment) origin() (string, error) {
	parsed, err := url.Parse(e.BaseURL)
	if err != nil {
		return "", fmt.Errorf("%w: the base URL cannot be read", ErrInvalidEnvironment)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: the base URL is not an http address of a host", ErrInvalidEnvironment)
	}

	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}
