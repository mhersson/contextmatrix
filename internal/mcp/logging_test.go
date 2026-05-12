package mcp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// TestMCPRequestInfoMiddleware verifies that mcpRequestInfoMiddleware correctly
// extracts JSON-RPC method and tool name from the request body and populates the
// MCPCall stored in context, while always restoring the body for downstream handlers.
func TestMCPRequestInfoMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		httpMethod    string
		body          string
		wantMethod    string
		wantTool      string
		bodyInContext bool // whether to inject MCPCall into the context
	}{
		{
			name:          "tools/call body populates method and tool",
			httpMethod:    http.MethodPost,
			body:          `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"claim_card"}}`,
			wantMethod:    "tools/call",
			wantTool:      "claim_card",
			bodyInContext: true,
		},
		{
			name:          "notifications/initialized body populates method only",
			httpMethod:    http.MethodPost,
			body:          `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
			wantMethod:    "notifications/initialized",
			wantTool:      "",
			bodyInContext: true,
		},
		{
			name:          "malformed JSON leaves fields empty",
			httpMethod:    http.MethodPost,
			body:          `not-json`,
			wantMethod:    "",
			wantTool:      "",
			bodyInContext: true,
		},
		{
			name:          "GET method is a no-op (call pointer unmodified)",
			httpMethod:    http.MethodGet,
			body:          `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"claim_card"}}`,
			wantMethod:    "",
			wantTool:      "",
			bodyInContext: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalBody := tc.body

			// Build context: inject an MCPCall if the test scenario requires it.
			ctx := t.Context()

			var call *ctxlog.MCPCall
			if tc.bodyInContext {
				ctx, call = ctxlog.WithMCPCall(ctx)
			}

			// Downstream handler: reads r.Body and asserts it equals the original bytes.
			downstream := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					got, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Equal(t, originalBody, string(got),
						"downstream should see the original body bytes")
				}
			})

			handler := mcpRequestInfoMiddleware(downstream)

			req := httptest.NewRequest(tc.httpMethod, "/mcp", strings.NewReader(tc.body))
			req = req.WithContext(ctx)
			rw := httptest.NewRecorder()

			handler.ServeHTTP(rw, req)

			if tc.bodyInContext {
				require.NotNil(t, call)
				assert.Equal(t, tc.wantMethod, call.Method, "Method mismatch")
				assert.Equal(t, tc.wantTool, call.Tool, "Tool mismatch")
			}
		})
	}
}

// TestMCPRequestInfoMiddleware_nilContext verifies that a nil MCPCall in context
// (no /mcp route injection) is handled defensively — middleware delegates to next
// without panicking.
func TestMCPRequestInfoMiddleware_nilContext(t *testing.T) {
	called := false
	downstream := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"method":"tools/call"}`, string(body))
	})

	handler := mcpRequestInfoMiddleware(downstream)

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewBufferString(`{"method":"tools/call"}`))
	// No MCPCall injected into context.

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	assert.True(t, called, "downstream should be called even when MCPCall is absent from context")
}
