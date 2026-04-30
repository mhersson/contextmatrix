package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPAuthMiddleware(t *testing.T) {
	const apiKey = "test-secret-key"

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Every auth failure collapses to the same 401 + WWW-Authenticate +
	// constant body so the response shape leaks no detail about which
	// check failed (missing header vs wrong token vs malformed scheme).
	const constAuthBody = `{"error":"unauthorized"}`

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantBody   string
		wantWWW    string // expected WWW-Authenticate header
	}{
		{
			name:       "valid Bearer token",
			authHeader: "Bearer test-secret-key",
			wantStatus: http.StatusOK,
			wantBody:   "ok",
		},
		{
			name:       "missing Authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantBody:   constAuthBody,
			wantWWW:    `Bearer realm="contextmatrix"`,
		},
		{
			name:       "wrong token",
			authHeader: "Bearer wrong-key",
			wantStatus: http.StatusUnauthorized,
			wantBody:   constAuthBody,
			wantWWW:    `Bearer realm="contextmatrix"`,
		},
		{
			name:       "non-Bearer scheme",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
			wantBody:   constAuthBody,
			wantWWW:    `Bearer realm="contextmatrix"`,
		},
		{
			name:       "Bearer prefix only",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
			wantBody:   constAuthBody,
			wantWWW:    `Bearer realm="contextmatrix"`,
		},
	}

	handler := mcpAuthMiddleware(inner, apiKey)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.wantBody)

			if tt.wantWWW != "" {
				assert.Equal(t, tt.wantWWW, w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestNewHandler_NoAPIKey_NoAuth(t *testing.T) {
	// When apiKey is empty, no auth middleware should be applied.
	// We test this by calling NewHandler with an empty key and verifying
	// the returned handler is not nil (basic smoke test).
	handler := NewHandler(nil, "")
	assert.NotNil(t, handler)
}
