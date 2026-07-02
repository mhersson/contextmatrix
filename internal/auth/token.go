// Package auth provides the cryptographic primitives for multi-user mode:
// random tokens (sessions, one-time links), argon2id password hashing, the
// master-key file, and AES-256-GCM secret encryption. It contains no HTTP
// or storage code.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// NewToken generates a 256-bit random token. raw is the value that travels
// in cookies and one-time URLs; hash is its hex SHA-256 — the only form that
// is ever persisted, so a leaked database yields no usable tokens.
func NewToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, HashToken(raw), nil
}

// HashToken returns the hex-encoded SHA-256 of a raw token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}
