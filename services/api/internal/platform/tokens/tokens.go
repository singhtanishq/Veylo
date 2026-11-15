// Package tokens generates cryptographically secure opaque tokens and their
// SHA-256 hashes for database storage. Raw tokens are never persisted.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// New returns (rawToken, tokenHash). The raw token is only ever present in
// emails / HTTP responses; the hash is what the database stores.
func New() (string, string) {
	raw := NewRaw()
	return raw, Hash(raw)
}

// NewRaw returns a 32-byte base64url token.
func NewRaw() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("tokens: crypto/rand failure: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash returns the hex SHA-256 of a raw token.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
