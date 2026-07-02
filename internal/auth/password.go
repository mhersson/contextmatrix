package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. These exceed the OWASP minimum recommendation
// (19 MiB, t=2, p=1). Raising them later is safe: every hash embeds its own
// parameters, VerifyPassword honours the embedded ones, and NeedsRehash lets
// the login path upgrade hashes transparently.
const (
	argonMemory  uint32 = 64 * 1024 // KiB → 64 MiB
	argonTime    uint32 = 3
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// ErrInvalidHash reports a stored password hash that is not a well-formed
// argon2id PHC string.
var ErrInvalidHash = errors.New("auth: invalid password hash encoding")

// HashPassword hashes a password with argon2id and returns a PHC-format
// string: $argon2id$v=19$m=…,t=…,p=…$<b64 salt>$<b64 key>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks password against a PHC-encoded argon2id hash using
// the parameters embedded in the hash. It returns (false, nil) for a wrong
// password and ErrInvalidHash for malformed input.
func VerifyPassword(password, encoded string) (bool, error) {
	_, m, t, p, salt, want, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want))) //nolint:gosec

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NeedsRehash reports whether encoded was produced with parameters weaker
// than the current ones (or is malformed) and should be re-hashed on the
// next successful login.
func NeedsRehash(encoded string) bool {
	_, m, t, p, _, _, err := parsePHC(encoded)
	if err != nil {
		return true
	}

	return m < argonMemory || t < argonTime || p < argonThreads
}

// parsePHC splits a $argon2id$v=…$m=…,t=…,p=…$salt$key string.
//
//nolint:unparam
func parsePHC(encoded string) (version int, m, t uint32, p uint8, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return 0, 0, 0, 0, nil, nil, ErrInvalidHash
	}

	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, 0, nil, nil, ErrInvalidHash
	}

	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &threads); err != nil || threads == 0 || threads > 255 {
		return 0, 0, 0, 0, nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, 0, nil, nil, ErrInvalidHash
	}

	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return 0, 0, 0, 0, nil, nil, ErrInvalidHash
	}

	return version, m, t, uint8(threads), salt, key, nil
}
