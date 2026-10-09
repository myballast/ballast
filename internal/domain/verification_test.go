// Written before the implementation: these are the rules the verification has to
// follow, and the one control that keeps Ballast from being aimed at somebody
// else's servers rests on them.
package domain

import (
	"errors"
	"strings"
	"testing"
)

func target(status VerificationStatus) Environment {
	return Environment{
		Name:               "staging",
		BaseURL:            "https://staging.example.test",
		VerificationStatus: status,
		VerificationToken:  "a-challenge-token",
		MaxVUs:             10,
		MaxDurationS:       60,
	}
}

// Proving and approving are two steps, and this is the half that says so: a
// valid proof is not permission to send load. Automatic approval falls the day
// somebody finds a way to serve a file on a domain that is not theirs, so the
// human step is what the product rests on.
func TestOnlyAnApprovedTargetTakesLoad(t *testing.T) {
	for status, allowed := range map[VerificationStatus]bool{
		VerificationPending:  false,
		VerificationProofOK:  false,
		VerificationFailed:   false,
		VerificationVerified: true,
		"":                   false,
		"something else":     false,
	} {
		if got := target(status).IsVerified(); got != allowed {
			t.Errorf("a %q target reports verified=%v", status, got)
		}
	}
}

func TestApprovingIsOnlyPossibleOnceTheProofIsIn(t *testing.T) {
	refused := []VerificationStatus{
		VerificationPending,
		VerificationFailed,
		"",
		"something else",
	}

	for _, status := range refused {
		if err := target(status).RequireApprovable(); !errors.Is(err, ErrNotProven) {
			t.Errorf("a %q target answered %v, want ErrNotProven", status, err)
		}
	}

	if err := target(VerificationProofOK).RequireApprovable(); err != nil {
		t.Errorf("a proven target could not be approved: %v", err)
	}
}

// Approving twice is refused rather than silently repeated: it would otherwise
// be a way to rewrite who approved a target, and when.
func TestApprovingAnApprovedTargetIsRefused(t *testing.T) {
	if err := target(VerificationVerified).RequireApprovable(); !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("approving an approved target answered %v, want ErrAlreadyVerified", err)
	}
}

// The trap this closes: recording a proof moves a target to proof_ok. Run on a
// target that is already approved, it would quietly take the approval away --
// an endpoint that reduces permissions while looking like it confirms them.
// Pointing the environment somewhere else is what resets it, and that is the
// edit path, not this one.
func TestAnApprovedTargetIsNotVerifiedAgain(t *testing.T) {
	if err := target(VerificationVerified).RequireProvable(); !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("verifying an approved target answered %v, want ErrAlreadyVerified", err)
	}
}

func TestAProofMayBeAttemptedAndRetried(t *testing.T) {
	for _, status := range []VerificationStatus{
		VerificationPending,
		// A failed attempt is worth retrying: the file may have just been put
		// in place, and that is the ordinary way this is used.
		VerificationFailed,
		// Proven but not yet approved: harmless, and refusing it would mean a
		// target whose file moved cannot be re-checked before approval.
		VerificationProofOK,
	} {
		if err := target(status).RequireProvable(); err != nil {
			t.Errorf("a %q target could not be verified: %v", status, err)
		}
	}
}

// The file lives at the root of the origin. RFC 8615 puts /.well-known there,
// so a base URL carrying a path must not drag it into the challenge URL --
// otherwise Ballast asks for a file at an address the target was never told
// about, and no verification would ever succeed for such an environment.
func TestTheChallengeIsReadAtTheRootOfTheOrigin(t *testing.T) {
	cases := map[string]string{
		"https://staging.example.test":            "https://staging.example.test/.well-known/ballast-a-challenge-token",
		"https://staging.example.test/":           "https://staging.example.test/.well-known/ballast-a-challenge-token",
		"https://staging.example.test/api/v2":     "https://staging.example.test/.well-known/ballast-a-challenge-token",
		"http://staging.example.test:8080/deeper": "http://staging.example.test:8080/.well-known/ballast-a-challenge-token",
	}

	for baseURL, want := range cases {
		env := target(VerificationPending)
		env.BaseURL = baseURL

		got, err := env.ProofURL()
		if err != nil {
			t.Fatalf("%s: %v", baseURL, err)
		}
		if got != want {
			t.Errorf("%s\n got  %s\n want %s", baseURL, got, want)
		}
	}
}

// A token that could carry a slash or a dot-dot would turn the challenge path
// into a different path than the one Ballast asks the user to create -- or into
// a traversal. The generator produces base64url, which has neither, and this is
// the check that keeps the two facts tied together.
func TestAChallengeURLIsRefusedWhenTheTokenCouldChangeThePath(t *testing.T) {
	for name, token := range map[string]string{
		"empty":        "",
		"a slash":      "abc/def",
		"a traversal":  "../../etc/passwd",
		"a query":      "abc?x=1",
		"a space":      "abc def",
		"a fragment":   "abc#frag",
		"a backslash":  `abc\def`,
		"an encoded /": "abc%2Fdef",
	} {
		env := target(VerificationPending)
		env.VerificationToken = token

		if _, err := env.ProofURL(); err == nil {
			t.Errorf("a token with %s was built into a URL", name)
		}
	}
}

// The host is what goes into the project's allowlist when an admin approves, so
// it is the hostname alone: a port is not a host, and the allowlist is about
// where traffic may go.
func TestTheHostAnApprovalAllows(t *testing.T) {
	cases := map[string]string{
		"https://staging.example.test":            "staging.example.test",
		"http://staging.example.test:8080/deeper": "staging.example.test",
		"https://STAGING.Example.Test":            "staging.example.test",
	}

	for baseURL, want := range cases {
		env := target(VerificationPending)
		env.BaseURL = baseURL

		got, err := env.Host()
		if err != nil {
			t.Fatalf("%s: %v", baseURL, err)
		}
		if got != want {
			t.Errorf("%s gave host %q, want %q", baseURL, got, want)
		}
	}
}

func TestAnUnreadableBaseURLHasNoHostAndNoChallenge(t *testing.T) {
	env := target(VerificationPending)
	env.BaseURL = "not a url at all"

	if _, err := env.Host(); err == nil {
		t.Error("a host was read out of something that is not a URL")
	}
	if _, err := env.ProofURL(); err == nil {
		t.Error("a challenge URL was built out of something that is not a URL")
	}
}

// Nothing about the verification may be explained by repeating the token in an
// error: it ends up in logs, and while a challenge token is not a credential,
// it is the one value whose secrecy stops somebody pre-serving a file for an
// environment they do not own.
func TestNoErrorRepeatsTheToken(t *testing.T) {
	const secretish = "a-challenge-token"

	env := target(VerificationVerified)
	for _, err := range []error{env.RequireProvable(), env.RequireApprovable()} {
		if err != nil && strings.Contains(err.Error(), secretish) {
			t.Errorf("an error repeats the token: %v", err)
		}
	}
}
