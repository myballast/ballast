package domain

import (
	"strings"
	"testing"
)

// Written before the redactor itself. What it protects is the one thing a log
// line must never carry out of a run: a credential the generator was handed, or
// a header a user's script printed from its own requests.

func TestASecretValueIsRemovedWhereverItAppears(t *testing.T) {
	redactor, _ := NewRedactor(map[string]string{"API_TOKEN": "s3cret-token-value"})

	line := redactor.Redact(
		"GET /orders token=s3cret-token-value retried with s3cret-token-value")

	if strings.Contains(line, "s3cret-token-value") {
		t.Fatalf("the secret survived: %q", line)
	}
	if got := strings.Count(line, Redacted); got != 2 {
		t.Fatalf("%d occurrence(s) were masked in %q, wanted 2", got, line)
	}
}

// The order matters when one value contains another: masking the short one
// first would leave the rest of the long one in the line.
func TestTheLongestSecretIsMaskedFirst(t *testing.T) {
	redactor, _ := NewRedactor(map[string]string{
		"SHORT": "abcd",
		"LONG":  "abcdefgh",
	})

	line := redactor.Redact("value=abcdefgh")

	if strings.Contains(line, "efgh") {
		t.Fatalf("part of the longer secret survived: %q", line)
	}
}

func TestAHeaderThatCarriesACredentialIsMasked(t *testing.T) {
	redactor, _ := NewRedactor(nil)

	cases := map[string]string{
		"an authorization header": `Authorization: Bearer eyJhbGciOi.payload.signature`,
		"a cookie":                `Cookie: ballast_session=abc; other=def`,
		"a set-cookie":            `set-cookie: ballast_session=abc; HttpOnly`,
		"shouted":                 `AUTHORIZATION: Basic dXNlcjpwYXNzd29yZA==`,
		"spaced out":              `authorization : Bearer eyJhbGciOi`,
		"inside some json":        `{"headers":{"authorization":"Bearer eyJhbGciOi"}}`,
		"a bearer token alone":    `retrying with Bearer eyJhbGciOi.payload`,
	}

	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			masked := redactor.Redact(line)

			for _, leaked := range []string{"eyJhbGciOi", "dXNlcjpwYXNzd29yZA==", "ballast_session=abc"} {
				if strings.Contains(masked, leaked) {
					t.Fatalf("%q survived in %q", leaked, masked)
				}
			}
			if !strings.Contains(masked, Redacted) {
				t.Fatalf("nothing was masked in %q", masked)
			}
		})
	}
}

// The other half of being useful: a line with nothing to hide comes back
// exactly as it was. A redactor that mangles ordinary output would be removed
// by whoever has to read the logs, and then nothing is protected.
func TestALineWithNothingToHideIsUntouched(t *testing.T) {
	redactor, _ := NewRedactor(map[string]string{"API_TOKEN": "s3cret-token-value"})

	cases := []string{
		`time="2026-10-12T10:00:00Z" level=info msg="starting"`,
		`  ✓ status is 200`,
		`default: 1 VUs  5s  iterations=42`,
		// The word on its own, with no separator after it: a message about
		// authorization checks is not an authorization header.
		`authorization checks passed: 42`,
		`cookies accepted by the target: 3`,
	}

	for _, line := range cases {
		if masked := redactor.Redact(line); masked != line {
			t.Errorf("an ordinary line was changed:\n  %q\n  %q", line, masked)
		}
	}
}

// A value of two characters is not a secret worth defending, and masking it
// would replace those two characters everywhere — destroying the logs while
// leaving the value guessable from the holes. It is reported instead, by key,
// so somebody can be told.
func TestAValueTooShortToMaskIsReportedRatherThanApplied(t *testing.T) {
	redactor, tooShort := NewRedactor(map[string]string{
		"PAGE_SIZE":   "ab",
		"REAL_SECRET": "a-real-secret-value",
	})

	if len(tooShort) != 1 || tooShort[0] != "PAGE_SIZE" {
		t.Fatalf("the short values reported are %v, wanted [PAGE_SIZE]", tooShort)
	}

	line := redactor.Redact("iterating over ab items with a-real-secret-value")
	if !strings.Contains(line, "over ab items") {
		t.Errorf("a two-character value was masked anyway: %q", line)
	}
	if strings.Contains(line, "a-real-secret-value") {
		t.Errorf("the real secret survived: %q", line)
	}
}

// What is reported names keys. A warning that printed the value would be the
// leak it exists to prevent.
func TestWhatIsReportedNamesKeysAndNeverValues(t *testing.T) {
	_, tooShort := NewRedactor(map[string]string{"TINY": "ab"})

	for _, reported := range tooShort {
		if strings.Contains(reported, "ab") {
			t.Fatalf("the report carries a value: %q", reported)
		}
	}
}

// The zero value is a redactor with no secrets to mask, not a broken one: a run
// against an environment that holds none still has headers to keep out of its
// logs.
func TestTheZeroRedactorStillMasksHeaders(t *testing.T) {
	var redactor Redactor

	if masked := redactor.Redact("Authorization: Bearer abc.def"); strings.Contains(masked, "abc.def") {
		t.Fatalf("the zero redactor let a credential through: %q", masked)
	}
}
