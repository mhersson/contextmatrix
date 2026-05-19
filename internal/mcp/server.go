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

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/mcp/mcpcontext"
	"github.com/mhersson/contextmatrix/internal/service"
)

// ServerConfig collects the dependencies for NewServer. ChatManager and
// ImageStore are optional and default to nil; when nil, the chat- and image-
// specific tool surfaces are not registered (or, for image attachments,
// get_card / get_task_context return text-only results).
type ServerConfig struct {
	Service           *service.CardService
	WorkflowSkillsDir string
	ChatManager       *chat.Manager
	ImageStore        images.Store
}

// NewServer creates a configured MCP server with all tools and prompts registered.
func NewServer(cfg ServerConfig) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "contextmatrix",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(registerToolsConfig{
		Server:            server,
		Service:           cfg.Service,
		WorkflowSkillsDir: cfg.WorkflowSkillsDir,
		ImageStore:        cfg.ImageStore,
	})
	registerPrompts(server, cfg.Service, cfg.WorkflowSkillsDir)

	if cfg.ChatManager != nil {
		registerChatRehydrationComplete(server, cfg.ChatManager)
	}

	return server
}

// NewHandler returns an http.Handler for MCP Streamable HTTP transport.
// If apiKey is non-empty, requests must include a matching Authorization: Bearer <key> header.
// Register this on POST /mcp in the router.
//
// Middleware order (outermost → innermost):
//
//	mcpAuthMiddleware → clearWriteDeadlineForStreaming → chatSessionHeaderMiddleware → mcpRequestInfoMiddleware → SDK handler
//
// Unauthenticated probes are rejected before body inspection. The write-deadline
// tweak stays outermost to apply to all authenticated requests. The chat-session
// header is stashed into context after auth so only authenticated callers can
// set it. Request info extraction runs just before the SDK so it can read the
// body once.
func NewHandler(server *mcp.Server, apiKey string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		// DisableLocalhostProtection allows requests with non-localhost Host
		// headers (e.g. host.docker.internal) when running behind Docker Desktop.
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	)
	// Innermost: SDK handler wrapped with request-info extraction.
	infoWrapped := mcpRequestInfoMiddleware(handler)
	// Above info: stash X-CM-Chat-Session into the request context so tool
	// handlers can gate session-scoped operations to the calling chat
	// container's own session.
	sessionWrapped := chatSessionHeaderMiddleware(infoWrapped)
	// Middle: write-deadline clearing for long-lived GET SSE streams.
	wrapped := clearWriteDeadlineForStreaming(sessionWrapped)
	if apiKey == "" {
		return wrapped
	}

	return mcpAuthMiddleware(wrapped, apiKey)
}

// chatSessionHeaderMiddleware reads the X-CM-Chat-Session header (forwarded by
// chat-container entrypoints) and stashes the value into the request context
// via mcpcontext.WithChatSession. Session-scoped MCP tools
// (chat_rehydration_complete) compare this against the in-RPC session_id to
// reject cross-session calls from a compromised or malicious caller.
//
// Empty header (card-mode worker, human curl) leaves the context untouched so
// existing flows are unaffected.
func chatSessionHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("X-CM-Chat-Session"); h != "" {
			r = r.WithContext(mcpcontext.WithChatSession(r.Context(), h))
		}

		next.ServeHTTP(w, r)
	})
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
					// Cap field lengths so a malicious client cannot amplify
					// each log line with a multi-megabyte method or tool name
					// (the outer bodyLimit caps the body at 5 MB, but the
					// extracted strings would otherwise be logged verbatim).
					// Real MCP method names are <40 chars; 64 is a comfortable
					// upper bound that matches the request_id ceiling used
					// elsewhere in router.go.
					call.Method = truncateLogField(msg.Method)
					if msg.Method == "tools/call" {
						call.Tool = truncateLogField(msg.Params.Name)
					}
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// maxLogFieldLen bounds the mcp_method / mcp_tool values written to the request
// log line. Real MCP method names and tool names are <40 chars; 64 leaves room
// for future growth without giving an authenticated client a log-amplification
// primitive via a multi-megabyte JSON-RPC payload.
const maxLogFieldLen = 64

// truncateLogField clips s to maxLogFieldLen runes. Truncation suffix is added
// only when truncation actually happened so short values are unchanged.
func truncateLogField(s string) string {
	if len(s) <= maxLogFieldLen {
		return s
	}

	return s[:maxLogFieldLen] + "…"
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
