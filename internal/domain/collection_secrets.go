package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// What decides that something in a collection is a secret. Three signals, and
// any one is enough: the environment file says so, the name has a word like
// "token" in it, or the value has the shape of a token.
//
// It errs towards calling something a secret. A variable wrongly made a secret
// costs a value typed in once; a token wrongly kept costs a leak.

// secretWords are the words, in a name, that mark a secret.
var secretWords = map[string]bool{
	"token": true, "password": true, "passwd": true, "pwd": true, "passphrase": true,
	"secret": true, "apikey": true, "key": true, "bearer": true, "credential": true,
	"credentials": true, "jwt": true, "authorization": true, "auth": true, "cookie": true,
}

var (
	camelBoundary   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	notAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// wordsOf cuts a name into lower-case words at every separator and every change
// of case: clientSecret, client_secret and client-secret all give client, secret.
func wordsOf(name string) []string {
	spaced := acronymBoundary.ReplaceAllString(name, "$1 $2")
	spaced = camelBoundary.ReplaceAllString(spaced, "$1 $2")

	var words []string
	for _, word := range notAlphanumeric.Split(spaced, -1) {
		if word != "" {
			words = append(words, strings.ToLower(word))
		}
	}
	return words
}

// looksLikeASecretName reports whether a name has a word that marks a secret.
//
// Whole words, not substrings: "author" and "keyword" contain "auth" and "key"
// and are not secrets.
func looksLikeASecretName(name string) bool {
	for _, word := range wordsOf(name) {
		if secretWords[word] {
			return true
		}
	}
	return false
}

var (
	jwtShape       = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*$`)
	tokenPrefix    = regexp.MustCompile(`^(ghp_|gho_|ghu_|ghs_|github_pat_|sk-|sk_live_|sk_test_|pk_live_|xox[bpas]-|AKIA[0-9A-Z]{16})`)
	longTokenShape = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{32,}$`)
	uuidShape      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// looksLikeASecretValue reports whether a value has the shape of a token: a JWT,
// a well-known prefix, or a long unbroken run of token characters. A value that
// holds a {{variable}} is a reference, not a literal, and is never one.
func looksLikeASecretValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "{{") {
		return false
	}
	if uuidShape.MatchString(trimmed) {
		return false
	}
	return jwtShape.MatchString(trimmed) || tokenPrefix.MatchString(trimmed) || longTokenShape.MatchString(trimmed)
}

// minGuardedLength is the shortest secret the final guard looks for. A shorter
// one would match inside ordinary words, and refusing an import because "pass"
// appeared in "password" would be the guard failing rather than protecting.
const minGuardedLength = 8

// secretBook is where an import keeps what it found to be secret: which
// variables, which environment key each one is read from, and every literal
// value it saw, for the guard at the end.
type secretBook struct {
	// keyOf maps a secret variable to the environment key it is read from.
	keyOf map[string]string
	// variableOf maps a literal value to the variable that stands for it, so
	// that the same token written twice is one secret.
	variableOf map[string]string
	// values are every secret literal seen anywhere in the file.
	values   []string
	findings []ImportFinding
}

func newSecretBook() *secretBook {
	return &secretBook{keyOf: map[string]string{}, variableOf: map[string]string{}}
}

// remember keeps a secret literal for the final guard.
func (b *secretBook) remember(value string) {
	if len(value) >= minGuardedLength {
		b.values = append(b.values, value)
	}
}

// isSecretVariable reports whether a variable is already a secret.
func (b *secretBook) isSecretVariable(variable string) bool {
	_, known := b.keyOf[variable]
	return known
}

// markVariable makes a variable a secret, and gives it its environment key.
func (b *secretBook) markVariable(variable string) {
	if b.isSecretVariable(variable) {
		return
	}
	b.keyOf[variable] = b.freshKey(variable)
}

// variableForLiteral returns the variable that stands for a secret literal,
// making one named after name when the value has not been seen before.
func (b *secretBook) variableForLiteral(name, literal string) string {
	b.remember(literal)

	if existing, seen := b.variableOf[literal]; seen {
		return existing
	}

	variable := safeVariableName(name)
	// Two different tokens must not share one variable: the second would be
	// read as the first when the secret is created.
	for b.isSecretVariable(variable) {
		variable += "_2"
	}

	b.markVariable(variable)
	b.variableOf[literal] = variable
	return variable
}

// freshKey derives the environment key a variable is read from — clientSecret
// becomes CLIENT_SECRET — and makes it one a secret may have. The prefix and the
// names Ballast injects are refused as secret keys, so those are renamed and the
// renaming is reported.
func (b *secretBook) freshKey(variable string) string {
	key := strings.ToUpper(strings.Join(wordsOf(variable), "_"))
	if key == "" || key[0] < 'A' || key[0] > 'Z' {
		key = "SECRET_" + key
	}

	if ValidateSecretKey(key) != nil {
		renamed := "COLLECTION_" + key
		b.findings = append(b.findings, ImportFinding{Message: fmt.Sprintf(
			"The variable %s would be read from the environment key %s, which Ballast does not allow for a secret, so the key is %s.",
			variable, key, renamed)})
		key = renamed
	}
	if len(key) > maxSecretKeyLength {
		key = key[:maxSecretKeyLength]
	}

	// Two variables that give the same key would read one secret.
	candidate := key
	for number := 2; b.keyTaken(candidate); number++ {
		suffix := fmt.Sprintf("_%d", number)
		candidate = key[:min(len(key), maxSecretKeyLength-len(suffix))] + suffix
	}
	return candidate
}

func (b *secretBook) keyTaken(key string) bool {
	for _, used := range b.keyOf {
		if used == key {
			return true
		}
	}
	return false
}

// secrets lists the secrets to create, for the variables a scenario uses, by key.
func (b *secretBook) secrets(used map[string]bool) []DetectedSecret {
	var found []DetectedSecret
	for variable, key := range b.keyOf {
		if used[variable] {
			found = append(found, DetectedSecret{Key: key, Variable: variable})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Key < found[j].Key })
	return found
}

// DetectedSecret is a secret the import found, and the environment key a
// scenario will read it from. It holds no value: the value stays in the file,
// and the person types it again where it belongs.
type DetectedSecret struct {
	Key      string
	Variable string
}
