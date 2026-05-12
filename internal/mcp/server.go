// Package mcp provides an MCP server exposing ContextMatrix tools and prompts
// via Streamable HTTP transport on POST /mcp.
package mcp

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
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
//
// Middleware order (outermost → innermost):
//
//	mcpAuthMiddleware → clearWriteDeadlineForStreaming → mcpRequestInfoMiddleware → SDK handler
//
// Unauthenticated probes are rejected before body inspection. The write-deadline
// tweak stays outermost to apply to all authenticated requests. Request info
// extraction runs just before the SDK so it can read the body once.
func NewHandler(server *mcp.Server, apiKey string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		// DisableLocalhostProtection allows requests with non-localhost Host
		// headers (e.g. host.docker.internal) when running behind Docker Desktop.
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	// Innermost: SDK handler wrapped with request-info extraction.
	infoWrapped := mcpRequestInfoMiddleware(handler)
	// Middle: write-deadline clearing for long-lived GET SSE streams.
	wrapped := clearWriteDeadlineForStreaming(infoWrapped)
	if apiKey == "" {
		return wrapped
	}

	return mcpAuthMiddleware(wrapped, apiKey)
}

// mcpRequestInfoMiddleware reads the JSON-RPC body to populate the MCPCall
// stored in context by the outer observe middleware. It is best-effort: any
// read or parse error is swallowed so logging never breaks MCP traffic.
//
// The body is fully restored before calling next so the SDK handler receives
// the original bytes unchanged.
func mcpRequestInfoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only POST carries a JSON-RPC body; GET/DELETE are SSE/session housekeeping.
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)

			return
		}

		call := ctxlog.MCPCallFromContext(r.Context())
		if call == nil {
			// Defensive: observe only injects MCPCall for /mcp; if we somehow
			// ended up mounted elsewhere, skip extraction rather than panic.
			next.ServeHTTP(w, r)

			return
		}

		// Read and restore the body so the SDK handler sees the original bytes.
		if r.Body != nil {
			buf, err := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(buf))

			if err == nil {
				// Minimal shape to extract method and (for tools/call) the tool name.
				var msg struct {
					Method string `json:"method"`
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if json.Unmarshal(buf, &msg) == nil {
					call.Method = msg.Method
					if msg.Method == "tools/call" {
						call.Tool = msg.Params.Name
					}
				}
			}
		}

		next.ServeHTTP(w, r)
	})
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
