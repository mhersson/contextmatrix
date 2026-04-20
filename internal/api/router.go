// Package api provides HTTP handlers for the ContextMatrix REST API.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const maxRequestBodySize = 1 << 20     // 1 MB
const mcpMaxBodySize = 5 * 1024 * 1024 // 5 MB

// Error codes for machine-parseable error responses.
const (
	ErrCodeProjectNotFound    = "PROJECT_NOT_FOUND"
	ErrCodeCardNotFound       = "CARD_NOT_FOUND"
	ErrCodeCardExists         = "CARD_EXISTS"
	ErrCodeInvalidTransition  = "INVALID_TRANSITION"
	ErrCodeValidationError    = "VALIDATION_ERROR"
	ErrCodeAlreadyClaimed     = "ALREADY_CLAIMED"
	ErrCodeNotClaimed         = "NOT_CLAIMED"
	ErrCodeAgentMismatch      = "AGENT_MISMATCH"
	ErrCodeDependenciesNotMet = "DEPENDENCIES_NOT_MET"
	ErrCodeProjectExists      = "PROJECT_EXISTS"
	ErrCodeProjectHasCards    = "PROJECT_HAS_CARDS"
	ErrCodeInternalError      = "INTERNAL_ERROR"
	ErrCodeBadRequest         = "BAD_REQUEST"
	ErrCodeHumanOnlyField     = "HUMAN_ONLY_FIELD"
	ErrCodeProtectedBranch    = "PROTECTED_BRANCH"
	ErrCodeInvalidSignature   = "INVALID_SIGNATURE"
	ErrCodeCardNotVetted      = "CARD_NOT_VETTED"
	ErrCodeAlreadyAutonomous  = "ALREADY_AUTONOMOUS"
	ErrCodeContentTooLarge    = "CONTENT_TOO_LARGE"
)

// APIError is the standard error response format.
type APIError struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// RouterConfig holds all dependencies for creating the HTTP router.
type RouterConfig struct {
	Service            *service.CardService
	Bus                *events.Bus
	CORSOrigin         string
	Syncer             Syncer
	Runner             *runner.Client
	RunnerCfg          config.RunnerConfig
	MCPAPIKey          string
	Port               int
	GitHubToken        string
	GitHubAPIBaseURL   string
	GitHubAllowedHosts []string
	SessionManager     *sessionlog.Manager // optional; enables card-scoped SSE log path
	Theme              string              // active color palette ("everforest" or "radix")
	Version            string              // build version string for display
}

// NewRouter creates a new HTTP router with all API routes registered.
// corsOrigin specifies the allowed CORS origin (e.g. "http://localhost:5173").
// If empty, CORS headers are not set.
func NewRouter(cfg RouterConfig) *http.ServeMux {
	mux := http.NewServeMux()

	// Create handlers
	ph := &projectHandlers{svc: cfg.Service, runnerEnabled: cfg.Runner != nil}
	ch := &cardHandlers{svc: cfg.Service}
	ah := &agentHandlers{svc: cfg.Service}
	eh := newEventHandlers(cfg.Bus)
	sh := &syncHandlers{syncer: cfg.Syncer}
	ach := &appConfigHandlers{theme: cfg.Theme, version: cfg.Version}
	bh := &branchHandlers{
		svc:              cfg.Service,
		githubToken:      cfg.GitHubToken,
		githubAPIBaseURL: cfg.GitHubAPIBaseURL,
		allowedHosts:     cfg.GitHubAllowedHosts,
		newBranchClient:  defaultBranchClient,
	}

	// Health check
	mux.HandleFunc("GET /healthz", handleHealthz)

	// Readiness check
	rdhz := &readinessHandlers{svc: cfg.Service}
	mux.HandleFunc("GET /readyz", rdhz.handleReadyz)

	// SSE events
	mux.HandleFunc("GET /api/events", eh.streamEvents)

	// Project routes
	mux.HandleFunc("GET /api/projects", ph.listProjects)
	mux.HandleFunc("POST /api/projects", ph.createProject)
	mux.HandleFunc("GET /api/projects/{project}", ph.getProject)
	mux.HandleFunc("PUT /api/projects/{project}", ph.updateProject)
	mux.HandleFunc("DELETE /api/projects/{project}", ph.deleteProject)

	// Card routes
	mux.HandleFunc("GET /api/projects/{project}/cards", ch.listCards)
	mux.HandleFunc("POST /api/projects/{project}/cards", ch.createCard)
	mux.HandleFunc("GET /api/projects/{project}/cards/{id}", ch.getCard)
	mux.HandleFunc("PUT /api/projects/{project}/cards/{id}", ch.updateCard)
	mux.HandleFunc("PATCH /api/projects/{project}/cards/{id}", ch.patchCard)
	mux.HandleFunc("DELETE /api/projects/{project}/cards/{id}", ch.deleteCard)

	// Agent routes
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/claim", ah.claimCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/release", ah.releaseCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/heartbeat", ah.heartbeatCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/log", ah.addLogEntry)
	mux.HandleFunc("GET /api/projects/{project}/cards/{id}/context", ah.getCardContext)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/usage", ah.reportUsage)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/report-push", ah.reportPush)

	// Project usage and dashboard
	mux.HandleFunc("GET /api/projects/{project}/usage", ph.getProjectUsage)
	mux.HandleFunc("GET /api/projects/{project}/dashboard", ph.getProjectDashboard)
	mux.HandleFunc("POST /api/projects/{project}/recalculate-costs", ph.recalculateCosts)

	// Branch listing
	mux.HandleFunc("GET /api/projects/{project}/branches", bh.listBranches)

	// App config
	mux.HandleFunc("GET /api/app/config", ach.getAppConfig)

	// Sync routes
	mux.HandleFunc("POST /api/sync", sh.triggerSync)
	mux.HandleFunc("GET /api/sync", sh.getSyncStatus)

	// Runner routes
	rh := &runnerHandlers{
		svc:            cfg.Service,
		runner:         cfg.Runner,
		runnerCfg:      cfg.RunnerCfg,
		mcpAPIKey:      cfg.MCPAPIKey,
		port:           cfg.Port,
		sessionManager: cfg.SessionManager,
	}
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/run", rh.runCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/stop", rh.stopCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/message", rh.messageCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/promote", rh.promoteCard)
	mux.HandleFunc("POST /api/projects/{project}/stop-all", rh.stopAll)
	// Only register runner-side endpoints when the runner is enabled.
	if cfg.Runner != nil {
		mux.HandleFunc("POST /api/runner/status", rh.runnerStatusUpdate)
		mux.HandleFunc("GET /api/runner/logs", rh.streamRunnerLogs)
	}

	// Apply middleware chain: recovery -> cors -> logging -> requestID -> bodyLimit -> handler
	return wrapMux(mux, cfg.CORSOrigin)
}

// wrapMux wraps the mux with all middleware.
func wrapMux(mux *http.ServeMux, corsOrigin string) *http.ServeMux {
	wrapper := http.NewServeMux()

	middlewares := []func(http.Handler) http.Handler{recovery, securityHeaders, logging, requestID, bodyLimit}
	if corsOrigin != "" {
		middlewares = []func(http.Handler) http.Handler{recovery, securityHeaders, corsMiddleware(corsOrigin), logging, requestID, bodyLimit}
	}

	wrapper.Handle("/", chain(mux, middlewares...))

	return wrapper
}

// chain applies middleware in order (first middleware is outermost).
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}

// requestID generates and attaches a unique request ID.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// logging logs each request with timing. Requests to /healthz and /readyz are
// served but not logged to avoid spamming logs with k8s health check noise.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)

			return
		}

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start),
			"request_id", w.Header().Get("X-Request-ID"),
		)
	})
}

// corsMiddleware returns a middleware that adds CORS headers for the given origin.
func corsMiddleware(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Agent-ID, X-Request-ID, X-Signature-256")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// recovery recovers from panics and returns 500.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					"error", err,
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
				)
				writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal server error", "")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds standard security headers to all responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// bodyLimitN returns a middleware that caps request body size to maxBytes.
// Requests whose Content-Length exceeds the limit are rejected immediately with 413.
// For streaming requests without Content-Length, http.MaxBytesReader enforces the
// limit when the body is read; bodyLimitN wraps the ResponseWriter to intercept the
// first write after a body-too-large error and ensure a 413 status is sent.
func bodyLimitN(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Reject immediately when Content-Length is declared and over limit.
			if r.ContentLength > maxBytes {
				writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge, "request body too large", "")

				return
			}

			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bodyLimit caps request body size to prevent OOM from large payloads.
var bodyLimit = bodyLimitN(maxRequestBodySize)

// WrapMCPHandler wraps an MCP handler with the recovery, logging, requestID,
// and body-size-limit middleware. The body limit is set to mcpMaxBodySize (5 MB)
// to accommodate large card bodies without exposing an unbounded upload surface.
func WrapMCPHandler(h http.Handler) http.Handler {
	return chain(h, recovery, logging, requestID, bodyLimitN(mcpMaxBodySize))
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter, enabling http.ResponseController
// to reach the connection for SetWriteDeadline/SetReadDeadline calls.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Flush implements http.Flusher by delegating to the underlying writer.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, code, message, details string) {
	writeJSON(w, status, APIError{
		Error:   message,
		Code:    code,
		Details: details,
	})
}

// handleServiceError maps service/storage errors to HTTP responses.
func handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, ErrCodeProjectNotFound, "project not found", "")
	case errors.Is(err, storage.ErrCardNotFound):
		writeError(w, http.StatusNotFound, ErrCodeCardNotFound, "card not found", "")
	case errors.Is(err, storage.ErrProjectExists):
		writeError(w, http.StatusConflict, ErrCodeProjectExists, "project already exists", "")
	case errors.Is(err, board.ErrInvalidProjectConfig),
		errors.Is(err, board.ErrMissingStalledState),
		errors.Is(err, board.ErrMissingStalledTransitions),
		errors.Is(err, board.ErrMissingNotPlannedState),
		errors.Is(err, board.ErrMissingNotPlannedTransitions):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid project config", err.Error())
	case errors.Is(err, storage.ErrProjectHasCards):
		writeError(w, http.StatusConflict, ErrCodeProjectHasCards, "project has cards", err.Error())
	case errors.Is(err, storage.ErrCardExists):
		writeError(w, http.StatusConflict, ErrCodeCardExists, "card already exists", "")
	case errors.Is(err, board.ErrDependenciesNotMet):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusConflict, ErrCodeDependenciesNotMet, "dependencies not met", details)
	case errors.Is(err, board.ErrInvalidTransition):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusConflict, ErrCodeInvalidTransition, "invalid state transition", details)
	case errors.Is(err, board.ErrInvalidType), errors.Is(err, board.ErrInvalidState), errors.Is(err, board.ErrInvalidPriority),
		errors.Is(err, board.ErrInvalidAutonomousConfig):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "validation error", details)
	case errors.Is(err, service.ErrInvalidPRUrl):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid PR URL", err.Error())
	case errors.Is(err, service.ErrReviewAttemptsCapped):
		writeError(w, http.StatusConflict, ErrCodeValidationError, "review attempts limit reached", err.Error())
	case errors.Is(err, service.ErrFieldTooLong):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "field exceeds maximum length", err.Error())
	case errors.Is(err, service.ErrProtectedBranch):
		writeError(w, http.StatusForbidden, ErrCodeProtectedBranch, "pushing to main/master is never allowed", "")
	case errors.Is(err, storage.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid path", err.Error())
	case errors.Is(err, lock.ErrAlreadyClaimed):
		writeError(w, http.StatusConflict, ErrCodeAlreadyClaimed, "card already claimed", err.Error())
	case errors.Is(err, lock.ErrNotClaimed):
		writeError(w, http.StatusConflict, ErrCodeNotClaimed, "card is not claimed", "")
	case errors.Is(err, lock.ErrAgentMismatch):
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent does not own this card", err.Error())
	case errors.Is(err, service.ErrCardNotVetted):
		writeError(w, http.StatusForbidden, ErrCodeCardNotVetted,
			"card not vetted", "externally imported cards must be vetted by a human before agents can claim them")
	case errors.Is(err, service.ErrCardTerminal):
		writeError(w, http.StatusConflict, ErrCodeInvalidTransition, "card is in a terminal state", err.Error())
	default:
		slog.Error("unhandled error", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal server error", "")
	}
}

// handleHealthz responds to health checks.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
