// Package mcp provides an MCP server exposing ContextMatrix tools and prompts
// via Streamable HTTP transport on POST /mcp.
package mcp

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/service"
)

// NewServer creates a configured MCP server with all tools and prompts registered.
func NewServer(svc *service.CardService, skillsDir string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "contextmatrix",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(server, svc, skillsDir)
	registerPrompts(server, svc, skillsDir)

	return server
}

// NewHandler returns an http.Handler for MCP Streamable HTTP transport.
// If apiKey is non-empty, requests must include a matching Authorization: Bearer <key> header.
// Register this on POST /mcp in the router.
func NewHandler(server *mcp.Server, apiKey string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		nil,
	)
	if apiKey == "" {
		return handler
	}
	return mcpAuthMiddleware(handler, apiKey)
}

// mcpAuthMiddleware wraps an http.Handler with Bearer token authentication.
func mcpAuthMiddleware(next http.Handler, apiKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, `{"error":"invalid Authorization format, expected Bearer <key>"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, prefix)
		if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
