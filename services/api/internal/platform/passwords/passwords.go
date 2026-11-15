// Package passwords implements Argon2id password hashing and verification.
package passwords

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters follow OWASP password-storage guidance (m=64 MiB, t=2, p=2).
const (
	memory     uint32 = 64 * 1024
	iterations uint32 = 2
	threads    uint8  = 2
	keyLen     uint32 = 32
	saltLen    uint32 = 16
)

var ErrInvalidHash = errors.New("passwords: invalid hash format")

// Hash derives an Argon2id hash for the password in the standard encoded form:
// $argon2id$v=19$m=65536,t=2,p=2$<salt-b64>$<hash-b64>
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passwords: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memory, threads, keyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, threads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks the password against an encoded hash in constant time.
func Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, ErrInvalidHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
