// Package ctxlog provides context-aware structured logging helpers.
// It stores a *slog.Logger enriched with a request_id attribute in
// the context so that all log lines emitted during a request share
// the same correlation ID without having to pass the logger explicitly.
package ctxlog

import (
	"context"
	"log/slog"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// mcpCallKey is the context key for storing *MCPCall.
type mcpCallKey struct{}

// MCPCall holds MCP-specific request metadata populated by mcpRequestInfoMiddleware.
// Both fields start empty and are filled in after the JSON-RPC body is parsed.
type MCPCall struct {
	Method string // JSON-RPC method (e.g. "tools/call", "notifications/initialized")
	Tool   string // tool name for tools/call requests; empty otherwise
}

// WithMCPCall stashes an empty *MCPCall in ctx and returns both the enriched
// context and the pointer. The caller (observe middleware) holds the pointer so
// downstream middleware (mcpRequestInfoMiddleware) can mutate it, and the
// pointer is readable after ServeHTTP returns.
func WithMCPCall(ctx context.Context) (context.Context, *MCPCall) {
	call := &MCPCall{}

	return context.WithValue(ctx, mcpCallKey{}, call), call
}

// MCPCallFromContext retrieves the *MCPCall stored by WithMCPCall, or nil if
// no value is present (e.g. non-MCP requests or tests that skip middleware).
func MCPCallFromContext(ctx context.Context) *MCPCall {
	call, _ := ctx.Value(mcpCallKey{}).(*MCPCall)

	return call
}

// WithRequestID returns a new context that carries a *slog.Logger
// derived from slog.Default() with the "request_id" attribute set to id.
// Retrieve the logger with Logger(ctx).
func WithRequestID(ctx context.Context, id string) context.Context {
	logger := slog.Default().With("request_id", id)

	return context.WithValue(ctx, contextKey{}, logger)
}

// Logger retrieves the *slog.Logger stored by WithRequestID. If no logger
// is found in ctx (e.g. background contexts or tests that skip middleware),
// it returns slog.Default() so call sites never receive a nil logger.
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}

	return slog.Default()
}
