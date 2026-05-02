package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawHTTPClient is a *http.Client that bypasses the test-only CSRF transport
// so the CSRF guard tests below see the request shape a real cross-origin
// browser tab would produce.
func rawHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport}
}

// TestCSRFGuard_RejectsPOSTWithoutHeader is the negative case: a POST that
// does not carry X-Requested-With must be rejected with 403 before the
// handler runs. A malicious tab opened by the user cannot set a custom
// header in a "simple request" without a CORS preflight, so the absence
// of the header is the signal we use to block it.
func TestCSRFGuard_RejectsPOSTWithoutHeader(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewReader([]byte(`{"title":"x","type":"task","priority":"medium"}`))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/projects/test-project/cards", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := rawHTTPClient().Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestCSRFGuard_AcceptsPOSTWithHeader is the positive case: when the header
// is present the handler runs as normal.
func TestCSRFGuard_AcceptsPOSTWithHeader(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	body := bytes.NewReader([]byte(`{"title":"x","type":"task","priority":"medium"}`))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/projects/test-project/cards", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := rawHTTPClient().Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

// TestCSRFGuard_AllowsGETWithoutHeader confirms read-only methods stay
// reachable without the header — the SOP already prevents a malicious tab
// from reading cross-origin GET responses.
func TestCSRFGuard_AllowsGETWithoutHeader(t *testing.T) {
	svc, bus, cleanup := testSetup(t)
	defer cleanup()

	server := httptest.NewServer(NewRouter(RouterConfig{Service: svc, Bus: bus}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/api/projects", nil)
	require.NoError(t, err)

	resp, err := rawHTTPClient().Do(req)

	require.NoError(t, err)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestCSRFGuard_ExemptionsAreExactPath is the regression guard for the
// exact-path allowlist (replacing the old strings.HasPrefix branches). A
// state-changing POST to a path that simply STARTS with /api/runner/ but
// is not in csrfMutatingExemptions must be blocked by the CSRF guard. The
// previous prefix-based design auto-exempted any future route under
// /api/runner/* even if it had no HMAC verification of its own.
func TestCSRFGuard_ExemptionsAreExactPath(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "unregistered subpath under /api/runner/", path: "/api/runner/status-fake"},
		{name: "unregistered subpath under /api/v1/", path: "/api/v1/cards/fake/X-1/something"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte("{}")))
			assert.False(t, csrfExempt(req),
				"path %q must not bypass the CSRF guard via prefix match", tc.path)
		})
	}
}

// TestCSRFGuard_ExemptionsAcceptListedPaths confirms that the registered
// HMAC-authenticated callbacks are still exempt under the new exact-match
// design.
func TestCSRFGuard_ExemptionsAcceptListedPaths(t *testing.T) {
	for path := range csrfMutatingExemptions {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte("{}")))
			assert.True(t, csrfExempt(req), "%q must remain exempt", path)
		})
	}
}
