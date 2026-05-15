// Package mcpcontext exposes typed context helpers shared by the MCP HTTP
// transport (server.go middleware) and the tool handlers. The session
// helper is set by chatSessionHeaderMiddleware when a chat-container
// caller forwards its X-CM-Chat-Session header; tools may use it to gate
// session-scoped operations to the caller's own session.
package mcpcontext

import "context"

type chatSessionKey struct{}

// WithChatSession returns a derived context carrying the chat session ID
// the caller claims to belong to. Set by middleware; read by tools.
func WithChatSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, chatSessionKey{}, sessionID)
}

// ChatSession returns the chat session ID stashed by WithChatSession.
// Empty string when the header was absent (card-mode, human curl).
func ChatSession(ctx context.Context) string {
	v, _ := ctx.Value(chatSessionKey{}).(string)

	return v
}
