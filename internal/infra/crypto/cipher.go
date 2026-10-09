package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeyLength is the length of the key, in bytes. AES-256, so thirty-two.
const KeyLength = 32

// Cipher seals and opens the secrets Ballast stores.
//
// AES-256-GCM: authenticated, so a value edited in the database fails to open
// rather than opening as something else, and bound to associated data, so a
// ciphertext only opens where it was written.
//
// A Cipher is safe for concurrent use, and holds the key for the life of the
// process. Rotation is deliberately absent: it needs a key identifier stored
// beside every ciphertext, and that is its own piece of work.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher returns a cipher over key, which must be exactly KeyLength bytes.
//
// There is no derivation from a passphrase on purpose. A key stretched out of
// whatever somebody typed hides how much of a key it really is, and the error
// below is a better moment to find that out than never.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeyLength {
		return nil, fmt.Errorf("the encryption key is %d bytes; it must be exactly %d", len(key), KeyLength)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build the block cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build the aead: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// KeyFromBase64 reads a key from the form it is configured in.
//
// Padding is optional, because that is the difference between two tools that
// both print a perfectly good key.
func KeyFromBase64(encoded string) ([]byte, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return nil, errors.New("the encryption key is empty")
	}

	key, err := base64.StdEncoding.WithPadding(base64.NoPadding).
		DecodeString(strings.TrimRight(trimmed, "="))
	if err != nil {
		return nil, fmt.Errorf("the encryption key is not base64: %w", err)
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("the encryption key decodes to %d bytes; it must be %d", len(key), KeyLength)
	}

	return key, nil
}

// Seal encrypts plaintext and returns the nonce followed by the ciphertext.
//
// aad is authenticated but not encrypted: it does not travel with the value, it
// has to be supplied again to open it, and anything that does not match refuses.
// Passing what identifies the row — the organization, the environment, the key —
// is what keeps a ciphertext from being moved to another row by somebody with
// write access to the table and no key at all.
//
// Nothing of plaintext appears in any error this returns.
func (c *Cipher) Seal(aad []byte, plaintext string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read a nonce: %w", err)
	}

	// The nonce is prefixed to the ciphertext rather than stored in its own
	// column: it is meaningless on its own and must never be reused, so keeping
	// the two together is what makes a copy of one impossible without the other.
	return c.aead.Seal(nonce, nonce, []byte(plaintext), aad), nil
}

// Open decrypts what Seal produced, under the same associated data.
//
// It fails on a wrong key, a modified ciphertext, or associated data that does
// not match — without saying which, because the three are the same answer.
func (c *Cipher) Open(aad, sealed []byte) (string, error) {
	size := c.aead.NonceSize()
	if len(sealed) <= size {
		return "", errors.New("the stored value is too short to be a ciphertext")
	}

	plaintext, err := c.aead.Open(nil, sealed[:size], sealed[size:], aad)
	if err != nil {
		return "", errors.New("the stored value could not be decrypted")
	}

	return string(plaintext), nil
}
