package domain

import (
	"errors"
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

// ProofObserved says what a target answered when its challenge was fetched: a
// status and a size, and deliberately nothing of what the body said.
//
// The verifier reads a URL somebody else chose. Repeating what the page held
// would turn the verification into a way to read pages through Ballast, so the
// type has nowhere to put content, and a test holds it to that.
type ProofObserved struct {
	// Status is the HTTP status the target answered, and zero when it answered
	// nothing: unreachable, refused, redirected away.
	Status int
	// Bytes is how much the file held, when it was read.
	Bytes int
	// Truncated reports a file longer than the verifier is willing to read.
	Truncated bool
	// Err is why the proof failed, in the verifier's own words.
	Err error
}

func (o *ProofObserved) Error() string { return o.Err.Error() }
func (o *ProofObserved) Unwrap() error { return o.Err }

// ProofFailure is what a failed verification tells whoever has to fix it: where
// Ballast looked, what answered, and how it differed from the token.
//
// The URL carries the token. It is for the screen of somebody who manages the
// environment — they are the one who has to publish it — and never for the
// journal or the logs.
type ProofFailure struct {
	// URL is exactly where the challenge was looked for.
	URL string
	// Reason is the verifier's sentence, chosen by that code.
	Reason string
	// Status is zero when the target never answered.
	Status    int
	Bytes     int
	Truncated bool
}

func (f *ProofFailure) Error() string { return f.Reason }

// DescribeProofFailure builds the description of a failed fetch of proofURL.
func DescribeProofFailure(proofURL string, err error) *ProofFailure {
	failure := &ProofFailure{URL: proofURL, Reason: err.Error()}

	var seen *ProofObserved
	if errors.As(err, &seen) {
		failure.Status, failure.Bytes, failure.Truncated = seen.Status, seen.Bytes, seen.Truncated
	}
	return failure
}

// SearchedPath is where the challenge was looked for, with the token replaced by
// a placeholder: "https://host/.well-known/ballast-<token>".
//
// What leaves the service in an error body. The token is not a credential — it
// is meant to be published — but an error body is copied into tickets and chat,
// and the whole point of the token being unguessable is that nobody serves a
// file for a target they do not own. The person fixing the failure already has
// the token in front of them, in the challenge; what they need here is the host
// and the path.
func (f ProofFailure) SearchedPath() string {
	parsed, err := url.Parse(f.URL)
	if err != nil {
		return ""
	}

	if i := strings.LastIndex(parsed.Path, wellKnownPath+proofPrefix); i >= 0 {
		parsed.Path = parsed.Path[:i] + wellKnownPath + proofPrefix + "<token>"
	}
	parsed.RawPath = ""
	// url.String would escape the angle brackets; the placeholder is for reading.
	return strings.ReplaceAll(parsed.String(), "%3Ctoken%3E", "<token>")
}
