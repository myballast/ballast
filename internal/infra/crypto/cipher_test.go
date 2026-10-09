package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/infra/crypto"
)

func key(t *testing.T) []byte {
	t.Helper()

	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return k
}

func newCipher(t *testing.T) *crypto.Cipher {
	t.Helper()

	c, err := crypto.NewCipher(key(t))
	if err != nil {
		t.Fatalf("build the cipher: %v", err)
	}
	return c
}

const (
	token = "ghp_a_token_that_would_be_worth_stealing"
	aad   = "org/environment/API_TOKEN"
)

func TestASealedSecretComesBackAsItself(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	sealed, err := c.Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	opened, err := c.Open([]byte(aad), sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != token {
		t.Fatalf("opened %q, sealed %q", opened, token)
	}
}

// The column is bytea and what goes into it must be unreadable. Worth asserting
// rather than assuming: a cipher wired up wrong — an empty key, a mode that
// turns out to be a copy — fails exactly here and nowhere else.
func TestTheCiphertextDoesNotCarryThePlaintext(t *testing.T) {
	t.Parallel()

	sealed, err := newCipher(t).Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if bytes.Contains(sealed, []byte(token)) {
		t.Fatal("the plaintext is readable in the ciphertext")
	}
	if bytes.Contains(sealed, []byte("ghp_")) {
		t.Fatal("part of the plaintext is readable in the ciphertext")
	}
}

// Same value, same key, different ciphertext: the nonce is fresh every time.
// Without that, equal secrets are visible as equal to anybody reading the table,
// and in GCM a repeated nonce loses the key.
func TestSealingTheSameSecretTwiceGivesTwoDifferentCiphertexts(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	first, err := c.Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := c.Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal again: %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("two seals of the same value are identical; the nonce is not fresh")
	}
}

func TestAnotherKeyCannotOpenIt(t *testing.T) {
	t.Parallel()

	sealed, err := newCipher(t).Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := newCipher(t).Open([]byte(aad), sealed); err == nil {
		t.Fatal("a different key opened the secret")
	}
}

// Authenticated encryption, not just encryption: a value edited in the database
// must fail to open rather than open as something else.
func TestASingleAlteredByteIsRefused(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	sealed, err := c.Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	for _, at := range []int{0, len(sealed) / 2, len(sealed) - 1} {
		tampered := bytes.Clone(sealed)
		tampered[at] ^= 0x01

		if _, err := c.Open([]byte(aad), tampered); err == nil {
			t.Fatalf("a ciphertext altered at byte %d opened", at)
		}
	}
}

// The associated data ties a ciphertext to the one place it belongs. Somebody
// with write access to the table could otherwise copy the value of a secret from
// an environment they own into another organization's, and read it back by
// launching a run — without ever holding the key.
func TestACiphertextMovedSomewhereElseIsRefused(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	sealed, err := c.Seal([]byte("org-a/environment-1/API_TOKEN"), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	for _, elsewhere := range []string{
		"org-b/environment-1/API_TOKEN",
		"org-a/environment-2/API_TOKEN",
		"org-a/environment-1/OTHER_TOKEN",
		"",
	} {
		if _, err := c.Open([]byte(elsewhere), sealed); err == nil {
			t.Fatalf("the ciphertext opened under %q", elsewhere)
		}
	}
}

func TestSomethingTooShortToBeACiphertextIsRefused(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	for name, sealed := range map[string][]byte{
		"nothing at all":   {},
		"a nonce and less": make([]byte, 12),
		"half a nonce":     make([]byte, 6),
	} {
		if _, err := c.Open([]byte(aad), sealed); err == nil {
			t.Fatalf("%s opened", name)
		}
	}
}

func TestAKeyOfTheWrongLengthIsRefused(t *testing.T) {
	t.Parallel()

	for name, k := range map[string][]byte{
		"empty":         {},
		"sixteen bytes": make([]byte, 16),
		"thirty-one":    make([]byte, 31),
		"thirty-three":  make([]byte, 33),
		"a passphrase":  []byte("correct horse battery staple"),
	} {
		if _, err := crypto.NewCipher(k); err == nil {
			t.Fatalf("a key of %s was accepted", name)
		}
	}
}

func TestReadingTheKeyFromItsConfiguredForm(t *testing.T) {
	t.Parallel()

	raw := key(t)
	encoded := base64.StdEncoding.EncodeToString(raw)

	read, err := crypto.KeyFromBase64(encoded)
	if err != nil {
		t.Fatalf("read the key: %v", err)
	}
	if !bytes.Equal(read, raw) {
		t.Fatal("the key read back is not the key written")
	}

	// Base64 without padding is what several tools hand out, so it is accepted
	// too rather than failing on something that is plainly a key.
	if _, err := crypto.KeyFromBase64(strings.TrimRight(encoded, "=")); err != nil {
		t.Fatalf("unpadded base64 was refused: %v", err)
	}

	for name, bad := range map[string]string{
		"empty":           "",
		"not base64":      "this is not base64 at all!",
		"too few bytes":   base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"too many bytes":  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		"only whitespace": "   ",
	} {
		if _, err := crypto.KeyFromBase64(bad); err == nil {
			t.Fatalf("a key that is %s was accepted", name)
		}
	}
}

// Nothing in an error may repeat what was being sealed: these errors reach the
// logs of a process that has just been handed a credential.
func TestAFailureNeverRepeatsTheSecret(t *testing.T) {
	t.Parallel()

	c := newCipher(t)

	sealed, err := c.Seal([]byte(aad), token)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	_, err = newCipher(t).Open([]byte(aad), sealed)
	if err == nil {
		t.Fatal("the wrong key opened the secret")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("the error repeats the secret: %v", err)
	}
}
