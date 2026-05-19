package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/images"
)

// TestBodyLimitOverride_AppliesToImageUpload verifies that POST /api/images
// uses the larger image-upload envelope cap rather than the 5 MB global cap.
// A 6 MB body is over the default cap but under imageUploadEnvelopeBytes, so
// the bodyLimit middleware must let it through (the handler will then return
// a non-413 status because the bytes are not valid multipart).
func TestBodyLimitOverride_AppliesToImageUpload(t *testing.T) {
	store, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	router := NewRouter(RouterConfig{ImageStore: store})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	body := bytes.Repeat([]byte("x"), 6*1024*1024)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/images", bytes.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "multipart/form-data; boundary=does-not-matter")
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	// bodyLimit must not reject — the handler will reject with 400 instead
	// because the 6 MB of "x" is not a valid multipart payload.
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, resp.StatusCode,
		"6 MB POST to /api/images must not 413 via bodyLimit override")
}

// TestBodyLimitOverride_DoesNotApplyToCards verifies that the per-route
// envelope cap is scoped to the image upload route only. A 6 MB POST to a
// card route must hit the 5 MB global cap and 413 before reaching the
// handler. ImageStore is wired in so the POST /api/images override is
// actually registered — without it, the override map would be empty and
// this test would not exercise the scoping invariant it claims to.
func TestBodyLimitOverride_DoesNotApplyToCards(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	store, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	router := NewRouter(RouterConfig{Service: svc, Bus: bus, ImageStore: store})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	body := bytes.Repeat([]byte("x"), 6*1024*1024)

	req, err := http.NewRequest(http.MethodPost,
		server.URL+"/api/projects/test-project/cards", bytes.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode,
		"6 MB POST to a card route must 413 — only /api/images opts into the larger cap")
}

// TestValidateOverrideLimit_PanicsOnTooSmall locks down the invariant that
// bodyLimitN's short-circuit relies on: per-route overrides must strictly
// raise the global cap. If a future maintainer accidentally drops the guard
// in validateOverrideLimit or registerWithBodyLimit, this test fails before
// the server boot path could silently swallow a smaller override.
func TestValidateOverrideLimit_PanicsOnTooSmall(t *testing.T) {
	cases := []struct {
		name  string
		limit int64
	}{
		{"equal to global cap", maxRequestBodySize},
		{"below global cap", maxRequestBodySize - 1},
		{"zero", 0},
		{"negative", -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Panics(t, func() {
				validateOverrideLimit("POST /api/test", tc.limit)
			}, "validateOverrideLimit must panic when limit <= maxRequestBodySize")
		})
	}
}

// TestValidateOverrideLimit_AcceptsLargerLimit confirms the happy path: a
// limit strictly greater than the global cap (matching the only legitimate
// caller's imageUploadEnvelopeBytes) does NOT panic.
func TestValidateOverrideLimit_AcceptsLargerLimit(t *testing.T) {
	assert.NotPanics(t, func() {
		validateOverrideLimit("POST /api/test", maxRequestBodySize+1)
	})
	assert.NotPanics(t, func() {
		validateOverrideLimit("POST /api/images", imageUploadEnvelopeBytes)
	})
}
