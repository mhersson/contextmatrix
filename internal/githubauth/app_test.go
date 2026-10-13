package githubauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genTestKey produces a 2048-bit RSA key for use in tests.
func genTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return key
}

// writeKeyToTempFile encodes key as PEM and returns the path of a
// temp file containing it. The file is auto-removed at test end.
func writeKeyToTempFile(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "private-key.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))

	return path
}

func TestNewAppProvider_OK(t *testing.T) {
	key := genTestKey(t)
	path := writeKeyToTempFile(t, key)

	p, err := NewAppProvider(123, 456, path)
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNewAppProvider_BadKeyPath(t *testing.T) {
	_, err := NewAppProvider(123, 456, "/does/not/exist/key.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read private key")
}

func TestNewAppProvider_BadKeyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a pem"), 0o600))

	_, err := NewAppProvider(123, 456, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse private key")
}

func TestNewAppProviderWithKey_NilKey(t *testing.T) {
	_, err := NewAppProviderWithKey(123, 456, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private key is nil")
}

func TestAppProvider_CreateJWT(t *testing.T) {
	key := genTestKey(t)
	p, err := NewAppProviderWithKey(123, 456, key, "")
	require.NoError(t, err)

	jwt, err := p.createJWT()
	require.NoError(t, err)
	assert.NotEmpty(t, jwt)
	// JWT format: three base64 segments separated by dots.
	dots := 0

	for _, c := range jwt {
		if c == '.' {
			dots++
		}
	}

	assert.Equal(t, 2, dots, "JWT should have exactly two dots")
}

func TestAppProvider_GenerateToken_OK(t *testing.T) {
	expectedExpiry := time.Now().Add(50 * time.Minute).UTC().Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/app/installations/456/access_tokens", r.URL.Path)
		auth := r.Header.Get("Authorization")
		assert.True(t, strings.HasPrefix(auth, "Bearer "), "Authorization should be a Bearer JWT, got %q", auth)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_installtoken123",
			"expires_at": expectedExpiry,
		})
	}))
	defer server.Close()

	key := genTestKey(t)
	p, err := NewAppProviderWithKey(123, 456, key, server.URL)
	require.NoError(t, err)

	token, expiresAt, err := p.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ghs_installtoken123", token)
	assert.True(t, expiresAt.Equal(expectedExpiry),
		"expiry mismatch: got %s, want %s", expiresAt, expectedExpiry)
}

func TestAppProvider_GenerateToken_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "bad credentials"}`))
	}))
	defer server.Close()

	p, err := NewAppProviderWithKey(123, 456, genTestKey(t), server.URL)
	require.NoError(t, err)

	_, _, err = p.GenerateToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestAppProvider_GenerateToken_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	p, err := NewAppProviderWithKey(123, 456, genTestKey(t), server.URL)
	require.NoError(t, err)

	_, _, err = p.GenerateToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse token response")
}

func TestAppProvider_GenerateToken_EmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token": "", "expires_at": "2026-04-25T00:00:00Z"}`))
	}))
	defer server.Close()

	p, err := NewAppProviderWithKey(123, 456, genTestKey(t), server.URL)
	require.NoError(t, err)

	_, _, err = p.GenerateToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty token")
}

func TestAppProvider_WithAPIBaseURL_TrailingSlash(t *testing.T) {
	called := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token": "x", "expires_at": "2026-04-25T00:00:00Z"}`))
	}))
	defer server.Close()

	// Pass URL with trailing slash; provider should trim it.
	p, err := NewAppProviderWithKey(123, 456, genTestKey(t), server.URL+"/")
	require.NoError(t, err)

	_, _, err = p.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.True(t, called, "mock server should have received the request")
}
