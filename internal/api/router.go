// Package api provides HTTP handlers for the ContextMatrix REST API.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// maxRequestBodySize caps every inbound request body. MCP card payloads are
// the largest legitimate input, so the global cap is sized to that envelope.
const maxRequestBodySize = 5 * 1024 * 1024 // 5 MB

// Error codes for machine-parseable error responses.
//
// The code → HTTP-status mapping is part of the public API contract:
//
//   - ErrCodeBadRequest         → 400 (malformed input: bad JSON, missing
//     path/query param, unknown filter value)
//   - ErrCodeValidationError    → 422 (mutation body semantically invalid:
//     unknown type, unknown state, bad autonomous combo, empty message, ...)
//
// Do not reuse ErrCodeValidationError for 400-class failures — clients
// disambiguate by code, and collapsing the two broke that.
const (
	ErrCodeProjectNotFound      = "PROJECT_NOT_FOUND"
	ErrCodeCardNotFound         = "CARD_NOT_FOUND"
	ErrCodeParentNotFound       = "PARENT_NOT_FOUND"
	ErrCodeCardExists           = "CARD_EXISTS"
	ErrCodeInvalidTransition    = "INVALID_TRANSITION"
	ErrCodeValidationError      = "VALIDATION_ERROR"
	ErrCodeAlreadyClaimed       = "ALREADY_CLAIMED"
	ErrCodeNotClaimed           = "NOT_CLAIMED"
	ErrCodeAgentMismatch        = "AGENT_MISMATCH"
	ErrCodeDependenciesNotMet   = "DEPENDENCIES_NOT_MET"
	ErrCodeProjectExists        = "PROJECT_EXISTS"
	ErrCodeProjectHasCards      = "PROJECT_HAS_CARDS"
	ErrCodeInternalError        = "INTERNAL_ERROR"
	ErrCodeBadRequest           = "BAD_REQUEST"
	ErrCodeHumanOnlyField       = "HUMAN_ONLY_FIELD"
	ErrCodeProtectedBranch      = "PROTECTED_BRANCH"
	ErrCodeInvalidSignature     = "INVALID_SIGNATURE"
	ErrCodeCardNotVetted        = "CARD_NOT_VETTED"
	ErrCodeReviewAttemptsCapped = "REVIEW_ATTEMPTS_CAPPED"
	ErrCodeContentTooLarge      = "CONTENT_TOO_LARGE"
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
	JiraImporter       *jira.Importer
	JiraBaseURL        string
	MCPAPIKey          string
	Port               int
	GitHubToken        string
	GitHubAPIBaseURL   string
	GitHubAllowedHosts []string
	SessionManager     *sessionlog.Manager // optional; enables card-scoped SSE log path
	Theme              string              // active color palette ("everforest" or "radix")
	Version            string              // build version string for display
	MCPHandler         http.Handler        // optional; registered at POST/GET/DELETE /mcp when set
}

// NewRouter creates a new HTTP router with all API routes registered.
// corsOrigin specifies the allowed CORS origin (e.g. "http://localhost:5173").
// If empty, CORS headers are not set.
// Returns http.Handler (wraps mux with metrics and other middleware).
func NewRouter(cfg RouterConfig) http.Handler {
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
		mux.HandleFunc("GET /api/v1/cards/{project}/{id}/autonomous", rh.getCardAutonomous)
	}

	// Jira routes
	jh := &jiraHandlers{importer: cfg.JiraImporter, baseURL: cfg.JiraBaseURL}
	mux.HandleFunc("GET /api/jira/status", jh.status)

	if cfg.JiraImporter != nil {
		mux.HandleFunc("GET /api/jira/epic/{epicKey}", jh.previewEpic)
		mux.HandleFunc("POST /api/jira/import-epic", jh.importEpic)
	}

	// MCP server routes — registered on the inner mux so they share the
	// same middleware chain as every other route (recovery, requestID,
	// observe, bodyLimit, ...).
	if cfg.MCPHandler != nil {
		mux.Handle("POST /mcp", cfg.MCPHandler)
		mux.Handle("GET /mcp", cfg.MCPHandler)
		mux.Handle("DELETE /mcp", cfg.MCPHandler)
	}

	// Apply middleware chain. First entry is outermost:
	//   recovery -> securityHeaders -> [cors] -> requestID -> observe -> bodyLimit -> mux
	// requestID runs before observe so the request_id is in-context when the
	// request log line fires. observe sits inside recovery so any panic's
	// stack trace is logged with the request's context.
	middlewares := []func(http.Handler) http.Handler{recovery, securityHeaders, requestID, observe, bodyLimit}
	if cfg.CORSOrigin != "" {
		middlewares = []func(http.Handler) http.Handler{recovery, securityHeaders, corsMiddleware(cfg.CORSOrigin), requestID, observe, bodyLimit}
	}

	return chain(mux, middlewares...)
}

// observe records RED metrics and emits a per-request log line. Health probes
// (/healthz, /readyz) are skipped entirely to avoid log spam. SSE endpoints
// are logged but excluded from the REST latency histogram because their
// connection lifetime (minutes to hours) would drown out real latency signal.
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)

			return
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rw, r)

		dur := time.Since(start)

		ctxlog.Logger(r.Context()).Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration_ms", dur.Milliseconds(),
		)

		// SSE streams would pollute the REST latency histogram and the
		// path label set — skip them entirely for metrics. MCP Streamable
		// HTTP GET /mcp is a long-lived SSE connection for the same reason.
		if r.URL.Path == "/api/events" || r.URL.Path == "/api/runner/logs" ||
			(r.Method == http.MethodGet && r.URL.Path == "/mcp") {
			return
		}

		// r.Pattern is set by http.ServeMux on matched routes. Unmatched
		// routes (404s, bogus paths) collapse to a single "unmatched"
		// label value so an attacker cannot explode label cardinality by
		// hitting /foo/<random>.
		pattern := r.Pattern
		if pattern == "" {
			pattern = "unmatched"
		}

		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, pattern, strconv.Itoa(rw.statusCode)).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(dur.Seconds())
	})
}

// chain applies middleware in order (first middleware is outermost).
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}

// requestIDPattern bounds client-supplied X-Request-ID to a safe length and
// charset. Anything else gets a fresh UUID so untrusted input can neither
// bloat log lines nor smuggle unexpected characters into downstream systems.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// requestID honors a client-supplied X-Request-ID header when it matches
// requestIDPattern, otherwise generates a UUID. The id is echoed in the
// response header and stashed in a request-scoped logger in context.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = uuid.New().String()
		}

		w.Header().Set("X-Request-ID", id)
		ctx := ctxlog.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
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
				ctxlog.Logger(r.Context()).Error("panic recovered",
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
//
// Ordering is load-bearing: specific "resource not found" sentinels must be
// matched before generic board.Err* validation sentinels. ValidationError
// wraps a board.Err* sentinel, so errors.Is(err, board.ErrInvalidType) can
// also be true for a ValidationError that semantically represents a missing
// parent — without explicit ordering, the parent-not-found case would fall
// into the generic 422 branch and lie to the caller.
//
// The raw error is logged once at the top with the request's correlation ID
// so operators retain full context even when the client-facing message is
// sanitized or generic. Every branch that surfaces err.Error() as response
// details routes through sanitizeErrorDetails so filesystem paths / go-git
// transport messages don't leak to untrusted callers.
func handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctxlog.Logger(r.Context()).Error("service error", "error", err.Error())

	switch {
	// --- Not-found sentinels (404) ---
	case errors.Is(err, storage.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, ErrCodeProjectNotFound, "project not found", "")
	case errors.Is(err, storage.ErrCardNotFound):
		writeError(w, http.StatusNotFound, ErrCodeCardNotFound, "card not found", "")
	case errors.Is(err, board.ErrParentNotFound):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusNotFound, ErrCodeParentNotFound, "parent card not found", details)

	// --- Conflict sentinels (409) ---
	case errors.Is(err, storage.ErrProjectExists):
		writeError(w, http.StatusConflict, ErrCodeProjectExists, "project already exists", "")
	case errors.Is(err, storage.ErrProjectHasCards):
		writeError(w, http.StatusConflict, ErrCodeProjectHasCards, "project has cards", sanitizeErrorDetails(err))
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
	case errors.Is(err, service.ErrReviewAttemptsCapped):
		writeError(w, http.StatusConflict, ErrCodeReviewAttemptsCapped, "review attempts limit reached", sanitizeErrorDetails(err))
	case errors.Is(err, lock.ErrAlreadyClaimed):
		writeError(w, http.StatusConflict, ErrCodeAlreadyClaimed, "card already claimed", sanitizeErrorDetails(err))
	case errors.Is(err, lock.ErrNotClaimed):
		writeError(w, http.StatusConflict, ErrCodeNotClaimed, "card is not claimed", "")
	case errors.Is(err, service.ErrCardTerminal):
		writeError(w, http.StatusConflict, ErrCodeInvalidTransition, "card is in a terminal state", sanitizeErrorDetails(err))

	// --- Forbidden sentinels (403) ---
	case errors.Is(err, service.ErrProtectedBranch):
		writeError(w, http.StatusForbidden, ErrCodeProtectedBranch, "pushing to main/master is never allowed", "")
	case errors.Is(err, lock.ErrAgentMismatch):
		writeError(w, http.StatusForbidden, ErrCodeAgentMismatch, "agent does not own this card", sanitizeErrorDetails(err))
	case errors.Is(err, service.ErrCardNotVetted):
		writeError(w, http.StatusForbidden, ErrCodeCardNotVetted,
			"card not vetted", "externally imported cards must be vetted by a human before agents can claim them")

	// --- Bad-request sentinels (400) ---
	case errors.Is(err, storage.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid path", sanitizeErrorDetails(err))

	// --- Validation sentinels (422) — mutation body shape/semantics ---
	case errors.Is(err, board.ErrInvalidProjectConfig),
		errors.Is(err, board.ErrMissingStalledState),
		errors.Is(err, board.ErrMissingStalledTransitions),
		errors.Is(err, board.ErrMissingNotPlannedState),
		errors.Is(err, board.ErrMissingNotPlannedTransitions):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid project config", sanitizeErrorDetails(err))
	case errors.Is(err, board.ErrInvalidType), errors.Is(err, board.ErrInvalidState), errors.Is(err, board.ErrInvalidPriority),
		errors.Is(err, board.ErrInvalidAutonomousConfig):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "validation error", details)
	case errors.Is(err, service.ErrInvalidPRUrl):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid PR URL", sanitizeErrorDetails(err))
	case errors.Is(err, service.ErrFieldTooLong):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "field exceeds maximum length", sanitizeErrorDetails(err))

	default:
		slog.Error("unhandled error", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal server error", "")
	}
}

// handleHealthz responds to health checks.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
