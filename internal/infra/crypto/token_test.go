package crypto_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/infra/crypto"
)

func TestTwoTokensAreNeverTheSame(t *testing.T) {
	t.Parallel()

	tokens := crypto.NewRandomTokens()
	seen := map[string]bool{}

	for range 1000 {
		token, err := tokens.New()
		if err != nil {
			t.Fatalf("make a token: %v", err)
		}
		if seen[token] {
			t.Fatalf("the same token came back twice: %s", token)
		}
		seen[token] = true
	}
}

// The token goes into a path the target has to serve. One carrying a slash, or
// anything a URL escapes, would be asking for a different path than the one
// Ballast reads.
func TestATokenIsSafeInAPath(t *testing.T) {
	t.Parallel()

	token, err := crypto.NewRandomTokens().New()
	if err != nil {
		t.Fatalf("make a token: %v", err)
	}

	if token == "" {
		t.Fatal("the token is empty")
	}
	if url.PathEscape(token) != token {
		t.Fatalf("the token changes when escaped for a path: %s", token)
	}
	if strings.ContainsAny(token, "/+= ") {
		t.Fatalf("the token carries a character a path would mangle: %s", token)
	}
	// 32 bytes in base64 without padding.
	if len(token) != 43 {
		t.Fatalf("the token is %d characters; 32 random bytes is 43", len(token))
	}
}
