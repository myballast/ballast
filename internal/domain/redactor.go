package domain

import (
	"regexp"
	"sort"
	"strings"
)

// Redacted is what takes the place of anything a log line must not carry.
const Redacted = "[redacted]"

// minRedactableLength is the shortest secret value worth masking.
//
// Masking a two-character value would replace those two characters everywhere
// in the logs: the output becomes unreadable, and the value is guessable from
// the shape of the holes anyway. Such a value is not a secret that can be
// defended, so it is reported to whoever configured it instead of silently
// destroying what the screen exists to show.
const minRedactableLength = 4

// credentialHeaders is what gets masked whatever the line looks like.
//
// Everything after the separator goes, to the end of the line. That is
// deliberate and it is the right way to be wrong: a header value can hold
// spaces (`Basic dXNlcjpwYXNz`), semicolons (a cookie), quotes and JSON, so any
// rule that tries to stop at the end of the value is a guess — and a guess that
// stops too early leaks the rest. Losing the tail of a line that carried a
// credential costs nothing worth keeping.
//
// The separator has to follow the name, which is what keeps "authorization
// checks passed: 42" from being mistaken for a header.
var credentialHeaders = regexp.MustCompile(`(?i)("?\b(?:authorization|set-cookie|cookie)\b"?\s*[:=]\s*).*$`)

// bearerTokens catches a credential printed without its header name.
var bearerTokens = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)

// Redactor removes from a log line the things that must never be stored: the
// values of the secrets this run was handed, and any credential a script
// printed from its own requests.
//
// A script is code somebody uploaded: it can print whatever it likes, including
// the token it was given. The worker is the one place that knows those values —
// it injects them — so it is the one place that can take them back out.
//
// Its zero value masks headers and knows no secrets, which is what a run
// against an environment holding none needs.
type Redactor struct {
	// values are the secret values to mask, longest first. The order is what
	// makes a value that contains another safe: masking the short one first
	// would leave the tail of the long one in the line.
	values []string
}

// NewRedactor builds a redactor from an environment's secrets, and reports the
// keys whose values are too short to mask.
//
// The report names keys and never values: a warning that printed the value
// would be the leak this exists to prevent.
func NewRedactor(secrets map[string]string) (Redactor, []string) {
	var (
		values   []string
		tooShort []string
	)

	for key, value := range secrets {
		if len(value) < minRedactableLength {
			tooShort = append(tooShort, key)
			continue
		}
		values = append(values, value)
	}

	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	sort.Strings(tooShort)

	return Redactor{values: values}, tooShort
}

// Redact returns the line as it may be stored.
//
// Secrets first, then headers: a header value that happens to be one of this
// environment's secrets is masked either way, and doing values first keeps the
// masking of the rest of the line from hiding an occurrence somewhere else.
func (r Redactor) Redact(line string) string {
	for _, value := range r.values {
		line = strings.ReplaceAll(line, value, Redacted)
	}

	line = credentialHeaders.ReplaceAllString(line, "${1}"+Redacted)

	return bearerTokens.ReplaceAllString(line, "Bearer "+Redacted)
}
