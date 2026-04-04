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
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

const maxRequestBodySize = 1 << 20 // 1 MB

// Error codes for machine-parseable error responses.
const (
	ErrCodeProjectNotFound   = "PROJECT_NOT_FOUND"
	ErrCodeCardNotFound      = "CARD_NOT_FOUND"
	ErrCodeCardExists        = "CARD_EXISTS"
	ErrCodeInvalidTransition = "INVALID_TRANSITION"
	ErrCodeValidationError   = "VALIDATION_ERROR"
	ErrCodeAlreadyClaimed    = "ALREADY_CLAIMED"
	ErrCodeNotClaimed        = "NOT_CLAIMED"
	ErrCodeAgentMismatch     = "AGENT_MISMATCH"
	ErrCodeDependenciesNotMet = "DEPENDENCIES_NOT_MET"
	ErrCodeProjectExists      = "PROJECT_EXISTS"
	ErrCodeProjectHasCards    = "PROJECT_HAS_CARDS"
	ErrCodeInternalError      = "INTERNAL_ERROR"
	ErrCodeBadRequest         = "BAD_REQUEST"
	ErrCodeHumanOnlyField     = "HUMAN_ONLY_FIELD"
	ErrCodeProtectedBranch    = "PROTECTED_BRANCH"
	ErrCodeInvalidSignature   = "INVALID_SIGNATURE"
)

// APIError is the standard error response format.
type APIError struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// RouterConfig holds all dependencies for creating the HTTP router.
type RouterConfig struct {
	Service    *service.CardService
	Bus        *events.Bus
	CORSOrigin string
	Syncer     Syncer
	Runner     *runner.Client
	RunnerCfg  config.RunnerConfig
	MCPAPIKey  string
	Port       int
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
	sh := &syncHandlers{syncer: cfg.Syncer, apiKey: cfg.MCPAPIKey}

	// Health check
	mux.HandleFunc("GET /healthz", handleHealthz)

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

	// Sync routes
	mux.HandleFunc("POST /api/sync", sh.triggerSync)
	mux.HandleFunc("GET /api/sync", sh.getSyncStatus)

	// Runner routes
	rh := &runnerHandlers{svc: cfg.Service, runner: cfg.Runner, runnerCfg: cfg.RunnerCfg, mcpAPIKey: cfg.MCPAPIKey, port: cfg.Port}
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/run", rh.runCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/stop", rh.stopCard)
	mux.HandleFunc("POST /api/projects/{project}/stop-all", rh.stopAll)
	// Only register the runner status callback when the runner is enabled.
	if cfg.Runner != nil {
		mux.HandleFunc("POST /api/runner/status", rh.runnerStatusUpdate)
	}

	// Apply middleware chain: recovery -> cors -> logging -> requestID -> bodyLimit -> handler
	return wrapMux(mux, cfg.CORSOrigin)
}

// wrapMux wraps the mux with all middleware.
func wrapMux(mux *http.ServeMux, corsOrigin string) *http.ServeMux {
	wrapper := http.NewServeMux()
	middlewares := []func(http.Handler) http.Handler{recovery, logging, requestID, bodyLimit}
	if corsOrigin != "" {
		middlewares = []func(http.Handler) http.Handler{recovery, corsMiddleware(corsOrigin), logging, requestID, bodyLimit}
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

// logging logs each request with timing. Requests to /healthz are served but
// not logged to avoid spamming logs with k8s health check noise.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
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

// bodyLimit caps request body size to prevent OOM from large payloads.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		}
		next.ServeHTTP(w, r)
	})
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
	default:
		slog.Error("unhandled error", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "internal server error", "")
	}
}

// handleHealthz responds to health checks.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
