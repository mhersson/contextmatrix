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
// (no /mcp route injection) is handled defensively - middleware delegates to next
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

// TestMCPRequestInfoMiddleware_truncatesLongFields verifies that an
// authenticated client cannot use a multi-megabyte JSON-RPC method or tool name
// to amplify the per-request slog line. Both fields are capped at maxLogFieldLen.
func TestMCPRequestInfoMiddleware_truncatesLongFields(t *testing.T) {
	longMethod := strings.Repeat("M", maxLogFieldLen*4)
	longTool := strings.Repeat("T", maxLogFieldLen*4)

	// Build a body where method == "tools/call" so the tool branch fires, but
	// also include an oversize method via a separate body that uses the long
	// method literally.
	bodyToolsCall := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"` + longTool + `"}}`
	bodyLongMethod := `{"jsonrpc":"2.0","method":"` + longMethod + `","params":{}}`

	t.Run("tools/call with long tool name", func(t *testing.T) {
		ctx, call := ctxlog.WithMCPCall(t.Context())
		downstream := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})
		handler := mcpRequestInfoMiddleware(downstream)

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(bodyToolsCall))
		req = req.WithContext(ctx)

		handler.ServeHTTP(httptest.NewRecorder(), req)

		assert.Equal(t, "tools/call", call.Method)
		assert.LessOrEqual(t, len(call.Tool), maxLogFieldLen+len("…"),
			"Tool should be truncated to at most maxLogFieldLen + ellipsis")
	})

	t.Run("long method name", func(t *testing.T) {
		ctx, call := ctxlog.WithMCPCall(t.Context())
		downstream := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})
		handler := mcpRequestInfoMiddleware(downstream)

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(bodyLongMethod))
		req = req.WithContext(ctx)

		handler.ServeHTTP(httptest.NewRecorder(), req)

		assert.LessOrEqual(t, len(call.Method), maxLogFieldLen+len("…"),
			"Method should be truncated to at most maxLogFieldLen + ellipsis")
		assert.Empty(t, call.Tool, "Tool should be empty for non-tools/call methods")
	})
}
