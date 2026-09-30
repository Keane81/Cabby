// Package token issues the opaque bearer credentials of the auth service and maps them to
// the SHA-256 digest that is actually stored: a dump of cabber_session therefore yields no
// usable session token (research R-04).
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// rawLength is 256 bits, the entropy of an issued token.
const rawLength = 32

// New returns a fresh token: base64url of 32 random bytes, without padding characters so
// it survives a header value unchanged.
func New() (string, error) {
	raw := make([]byte, rawLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("token: read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Digest returns the bytes stored in cabber_session.token_hash for a presented token.
func Digest(presented string) []byte {
	sum := sha256.Sum256([]byte(presented))
	return sum[:]
}

// Equal compares two digests in constant time.
func Equal(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}
