package auth

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MasterKeyLen is the byte length of a master key - the format
// LoadMasterKey reads and LoadOrCreateMasterKey writes (hex-encoded).
const MasterKeyLen = 32

// KeyPurposeCredentials labels the HKDF derivation for credential-pool
// encryption. Every future encrypted store gets its own label so purposes
// never share key material.
const KeyPurposeCredentials = "credential-encryption" //nolint:gosec

// LoadMasterKey reads a hex-encoded 32-byte master key from path. It never
// creates anything: a missing file surfaces as an error wrapping
// os.ErrNotExist so callers can distinguish "no key yet" from a corrupt one.
func LoadMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: read master key: %w", err)
	}

	decoded, decErr := hex.DecodeString(strings.TrimSpace(string(data)))
	if decErr != nil || len(decoded) != MasterKeyLen {
		return nil, fmt.Errorf("auth: master key file %s: want %d hex-encoded bytes", path, MasterKeyLen)
	}

	return decoded, nil
}

// LoadOrCreateMasterKey reads a hex-encoded 32-byte master key from path.
// When the file does not exist it generates a fresh key, creates parent
// directories, and writes it hex-encoded with 0600 permissions. created
// reports whether a new key was written - callers should log prominently so
// operators know to move the file into proper secret management.
func LoadOrCreateMasterKey(path string) (key []byte, created bool, err error) {
	key, err = LoadMasterKey(path)

	switch {
	case err == nil:
		return key, false, nil

	case errors.Is(err, os.ErrNotExist):
		fresh := make([]byte, MasterKeyLen)
		if _, err := rand.Read(fresh); err != nil {
			return nil, false, fmt.Errorf("auth: generate master key: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, false, fmt.Errorf("auth: master key dir: %w", err)
		}

		if err := os.WriteFile(path, []byte(hex.EncodeToString(fresh)+"\n"), 0o600); err != nil {
			return nil, false, fmt.Errorf("auth: write master key: %w", err)
		}

		return fresh, true, nil

	default:
		return nil, false, err
	}
}

// DeriveKey derives a purpose-labeled 32-byte subkey from the master key via
// HKDF-SHA256. The master key itself is never used directly for encryption.
func DeriveKey(master []byte, purpose string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, master, nil, purpose, 32)
	if err != nil {
		return nil, fmt.Errorf("auth: derive %s key: %w", purpose, err)
	}

	return key, nil
}
