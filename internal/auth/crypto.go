package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrDecrypt reports a failed decryption: wrong key, truncated blob, or
// tampered ciphertext. GCM is authenticated, so tampering fails loudly
// instead of returning garbage.
var ErrDecrypt = errors.New("auth: decrypt failed")

// EncryptSecret encrypts plaintext with AES-256-GCM under a 32-byte key.
// The returned blob layout is nonce || ciphertext, with a fresh random nonce
// per call, so encrypting the same secret twice yields different blobs.
func EncryptSecret(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("auth: generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptSecret reverses EncryptSecret. It returns ErrDecrypt for a wrong
// key, truncated input, or tampered ciphertext.
func DecryptSecret(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	if len(blob) < gcm.NonceSize() {
		return nil, ErrDecrypt
	}

	nonce, ciphertext := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecrypt
	}

	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: gcm: %w", err)
	}

	return gcm, nil
}
