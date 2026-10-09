package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Secret is a credential a scenario needs to authenticate against a target.
//
// There is no field for the value, and that is the point. Every layer above the
// database speaks in this type, so no handler, no service and no log line can
// carry a secret's value by accident: there is nowhere to put it. The value
// travels exactly twice — in the request that sets it, and from the repository
// into the generator's environment — and never through here.
type Secret struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	Key           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const (
	maxSecretKeyLength   = 128
	maxSecretValueLength = 4096

	// generatorConfigPrefix is how k6 names the variables that configure k6
	// itself.
	generatorConfigPrefix = "K6_"
)

// BaseURLVariable is the variable Ballast injects to tell a scenario what to
// address. A scenario reads it instead of naming a host, which is what lets the
// same script run against staging and against production.
//
// Named here so that the list of keys a secret may not use and the list of
// variables Ballast injects are the same list.
const BaseURLVariable = "BASE_URL"

// secretKeyPattern is the shape of an environment variable name: upper case, a
// letter first, then letters, digits and underscores.
//
// Narrow on purpose. The key becomes one element of the container's environment,
// which is a flat list of KEY=VALUE strings, and anything outside this is either
// unusable or a way of smuggling a second variable into the list.
var secretKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ReservedSecretKeys are the variables Ballast injects itself.
//
// BASE_URL is the target whose ownership was proved and an administrator
// approved. A secret of the same name would be an attempt to point the load at
// something else, and while the injection order already settles who wins, a name
// that can never be stored cannot depend on that order staying right.
func ReservedSecretKeys() []string {
	return []string{BaseURLVariable}
}

// ValidateSecretKey refuses a key Ballast will not store.
//
// A secret is handed to the generator as an environment variable, and k6 reads
// its own configuration from environment variables at a higher precedence than
// the options of the module it runs. Ballast's load ceilings live in exactly
// those options: they are written into a generated entry module so that a user's
// own options are never consulted. A secret named K6_VUS or K6_DURATION would
// walk straight past them, and K6_OUT would stream the metrics somewhere of the
// caller's choosing. So the whole prefix is refused rather than a list of the
// variables k6 reads today, because such a list is a thing to forget the next
// time k6 gains one.
func ValidateSecretKey(key string) error {
	switch {
	case strings.TrimSpace(key) == "":
		return fmt.Errorf("%w: the key is empty", ErrInvalidSecret)
	case len(key) > maxSecretKeyLength:
		return fmt.Errorf("%w: the key is longer than %d characters",
			ErrInvalidSecret, maxSecretKeyLength)
	case !secretKeyPattern.MatchString(key):
		return fmt.Errorf(
			"%w: %q is not an environment variable name; use upper case letters, digits and underscores, starting with a letter",
			ErrInvalidSecret, key)
	case strings.HasPrefix(key, generatorConfigPrefix):
		return fmt.Errorf(
			"%w: %s configures k6 itself rather than the scenario, and would override the load ceilings this environment imposes",
			ErrInvalidSecret, key)
	}

	for _, reserved := range ReservedSecretKeys() {
		if key == reserved {
			return fmt.Errorf("%w: %s is injected by Ballast, from the target this environment points at",
				ErrInvalidSecret, key)
		}
	}

	return nil
}

// ValidateSecretValue refuses a value Ballast will not store.
//
// The bounds are what a credential looks like. A value also ends up as one
// element of the container's environment, where a null byte ends the string and
// a newline is not a separator — refusing both here is cheaper than reasoning
// about what every layer below does with them.
func ValidateSecretValue(value string) error {
	switch {
	case value == "":
		return fmt.Errorf("%w: the value is empty; remove the secret instead", ErrInvalidSecret)
	case len(value) > maxSecretValueLength:
		return fmt.Errorf("%w: the value is longer than %d bytes", ErrInvalidSecret, maxSecretValueLength)
	}

	// Nothing of the value itself is ever repeated in an error: it is the one
	// string in this package that must not reach a log.
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%w: the value carries a control character", ErrInvalidSecret)
	}

	return nil
}
