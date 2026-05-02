// Package mcp provides an MCP server exposing ContextMatrix tools and prompts
// via Streamable HTTP transport on POST /mcp.
package mcp

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/service"
)

// NewServer creates a configured MCP server with all tools and prompts registered.
func NewServer(svc *service.CardService, workflowSkillsDir string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "contextmatrix",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(server, svc, workflowSkillsDir)
	registerPrompts(server, svc, workflowSkillsDir)

	return server
}

// NewHandler returns an http.Handler for MCP Streamable HTTP transport.
// If apiKey is non-empty, requests must include a matching Authorization: Bearer <key> header.
// Register this on POST /mcp in the router.
func NewHandler(server *mcp.Server, apiKey string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		// DisableLocalhostProtection allows requests with non-localhost Host
		// headers (e.g. host.docker.internal) when running behind Docker Desktop.
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	// Wrap with write-deadline clearing for GET requests. The MCP Streamable HTTP
	// transport uses a long-lived SSE stream on GET /mcp. Like the SSE endpoint,
	// it must not be subject to the server's absolute WriteTimeout.
	wrapped := clearWriteDeadlineForStreaming(handler)
	if apiKey == "" {
		return wrapped
	}

	return mcpAuthMiddleware(wrapped, apiKey)
}

// clearWriteDeadlineForStreaming wraps an http.Handler and disables the write
// deadline for GET requests, which use long-lived SSE streaming. POST and DELETE
// requests are short-lived and keep the normal write timeout.
func clearWriteDeadlineForStreaming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			rc := http.NewResponseController(w)
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				// Non-fatal: log and continue. The connection may still work but
				// could be subject to the server's WriteTimeout.
				slog.Debug("MCP could not clear write deadline", "error", err)
			}
		}

		next.ServeHTTP(w, r)
	})
}

// mcpAuthMiddleware wraps an http.Handler with Bearer token authentication.
//
// Every authentication failure returns the same 401 + WWW-Authenticate +
// constant body. RFC 7235 says "Unauthorized" is the right code for a
// rejected credential; using 403 leaked a missing-vs-wrong-credential
// oracle that a probing attacker could exploit. Collapsing the body to
// a constant matches what CM does on the runner-status / autonomous
// endpoints.
func mcpAuthMiddleware(next http.Handler, apiKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")

		const prefix = "Bearer "

		if auth == "" || !strings.HasPrefix(auth, prefix) {
			writeMCPAuthFailure(w)

			return
		}

		token := strings.TrimPrefix(auth, prefix)
		if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			writeMCPAuthFailure(w)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeMCPAuthFailure emits the canonical 401 response for any MCP auth
// failure: header signals the realm, body is a constant string with no
// detail about which check failed.
func writeMCPAuthFailure(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="contextmatrix"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
