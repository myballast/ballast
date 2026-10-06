package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// tokenBytes is how much randomness a challenge token carries. Enough that
// guessing one is not a strategy, and short enough to paste into a filename.
const tokenBytes = 32

// RandomTokens makes the challenge an environment has to serve to prove that
// whoever declared it owns the host.
//
// Base64 in the URL alphabet, because the token ends up in a path: a token
// containing a slash would be a different path than the one Ballast asks for.
type RandomTokens struct{}

// NewRandomTokens returns the token source.
func NewRandomTokens() RandomTokens { return RandomTokens{} }

// New returns a fresh token. It reads the operating system's randomness, and an
// error from that is worth refusing an environment over: a predictable challenge
// proves nothing about who owns a host.
func (RandomTokens) New() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes for a verification token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}
