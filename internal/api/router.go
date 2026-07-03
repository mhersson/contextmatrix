// Package api provides HTTP handlers for the ContextMatrix REST API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/images"
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
	ErrCodeSyncDisabled         = "SYNC_DISABLED"
	ErrCodeSyncError            = "SYNC_ERROR"
	ErrCodeNoGitHubRepo         = "NO_GITHUB_REPO"
	// ErrCodeTooManySubscribers indicates the global SSE subscriber cap has
	// been reached; the client should back off and retry later. Mirrors the
	// per-session ErrCodeTooManyChats used by the chat hub.
	ErrCodeTooManySubscribers = "TOO_MANY_SUBSCRIBERS"

	// Multi-user auth. UNAUTHORIZED → 401 (no/expired session; the SPA
	// redirects to login). RATE_LIMITED → 429 with a Retry-After header.
	ErrCodeUnauthorized = "UNAUTHORIZED"
	ErrCodeRateLimited  = "RATE_LIMITED"

	// FORBIDDEN → 403: authenticated but lacking rights (non-admin on an
	// admin route). Distinct from AGENT_MISMATCH (card-claim ownership).
	ErrCodeForbidden = "FORBIDDEN"

	// USER_NOT_FOUND → 404: unknown username on an admin user-management
	// route. Deliberately distinct from ErrCodeCardNotFound — no card-flavored
	// code should leak into the auth/admin surface (brief's adaptation note).
	ErrCodeUserNotFound = "USER_NOT_FOUND"

	// Image upload + retrieval. Status mapping:
	//   IMAGE_NOT_FOUND        → 404 (unknown id or malformed id segment)
	//   IMAGE_UNSUPPORTED      → 415 (format not in png/jpeg/gif/webp)
	//   IMAGE_ANIMATED         → 415 (multi-frame GIF)
	//   IMAGE_MISSING_FILE     → 400 (multipart form missing `file` field)
	//   IMAGE_INVALID_PAYLOAD  → 400 (malformed multipart body or read failure)
	// Oversize uploads share the global CONTENT_TOO_LARGE (413) so clients
	// can disambiguate by status, not by code, on size-related rejections.
	ErrCodeImageNotFound       = "IMAGE_NOT_FOUND"
	ErrCodeImageUnsupported    = "IMAGE_UNSUPPORTED"
	ErrCodeImageAnimated       = "IMAGE_ANIMATED"
	ErrCodeImageMissingFile    = "IMAGE_MISSING_FILE"
	ErrCodeImageInvalidPayload = "IMAGE_INVALID_PAYLOAD"
)

// APIError is the standard error response format.
type APIError struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// RouterConfig holds all dependencies for creating the HTTP router.
type RouterConfig struct {
	Service                *service.CardService
	Bus                    *events.Bus
	CORSOrigin             string
	Syncer                 Syncer
	Runner                 TaskBackend          // nil when no task backend is configured
	BackendCfg             config.BackendConfig // resolved task-backend entry (Name set); zero value when Runner is nil
	MCPAPIKey              string
	GitHubTokenProvider    githubauth.TokenGenerator
	TaskSkillsDir          string // absolute path to the task-skills directory; empty disables the skills selector
	TaskSkillsGitRemoteURL string // configured git remote URL for the task-skills repo; fallback when dir is not a checkout
	GitHubAPIBaseURL       string
	GitHubAllowedHosts     []string
	SessionManager         *sessionlog.Manager // optional; enables card-scoped SSE log path
	Theme                  string              // active color palette ("everforest" or "radix")
	Version                string              // build version string for display
	MCPHandler             http.Handler        // optional; registered at POST/GET/DELETE /mcp when set
	ChatManager            *chat.Manager       // optional; enables /api/chats routes
	ChatHub                *chat.SSEHub        // optional; required when ChatManager is set
	ChatConfig             *config.ChatConfig  // optional; carries model allowlist for /api/chats endpoints
	// ChatBackendCfg is the dedicated "chat" backend entry. Its HMAC key
	// authenticates GET /api/chat/task-skills-source (the chat service's
	// pointer fetch). Zero value when no dedicated chat backend is configured.
	ChatBackendCfg config.BackendConfig
	// ImageStore is required in production — main.go always opens a
	// SQLite-backed store and wires it in unconditionally. Tests that do
	// not exercise /api/images may omit it; the routes are then unregistered
	// and the body-limit envelope still treats /api/images as a 404.
	ImageStore images.Store

	// Catalog and Blacklist supply model-selection inputs for agent-backend
	// triggers (attached as SelectionContext on TriggerPayload). Both are nil
	// until T8 wires the real implementations in main.go; runCard guards on
	// Catalog != nil before attaching Selection, so omitting them is safe.
	Catalog   catalogProvider
	Blacklist blacklistReader

	// ChatEndpointModels, when non-nil, is the raw (uncached) upstream fetch for
	// the openai-endpoint model list. Set when llm_endpoint.type == "openai".
	// NewRouter wraps it with a TTL cache via newCachedEndpointFetcher. The
	// neutral EndpointModelView keeps modelcatalog independent of the api package.
	ChatEndpointModels func(context.Context) ([]EndpointModelView, error)

	// ServedModels, when non-nil, returns the picker/validation model set from
	// the catalog builder: the vendor-screened OpenRouter list, or the
	// endpoint's served list. Feeds the openrouter-mode chat picker and
	// GET /api/models.
	ServedModels func(context.Context) []ServedModelView
	// ServedModelsSource labels ServedModels for GET /api/models: "openrouter"
	// or "endpoint". Empty when ServedModels is nil.
	ServedModelsSource string
	// ValidateChatModel, when non-nil, reports whether a model slug is in the
	// served set (fail-open on an empty catalog).
	ValidateChatModel func(context.Context, string) bool

	// AuthService enables multi-user mode: the session guard middleware and
	// the /api/auth/* routes are installed only when it is non-nil. A nil
	// AuthService leaves the router byte-for-byte identical to single-user
	// CM — that is the auth.mode "none" guarantee.
	AuthService *auth.Service
	// AuthMode is surfaced in GET /api/app/config ("multi"/"none"); empty
	// is reported as "none".
	AuthMode string
	// CredentialExists looks up a name in the instance credential pool, for
	// validating .board.yaml github_credential bindings on project update.
	// nil in none mode (mirrors AuthService's nil-in-none-mode contract).
	CredentialExists func(ctx context.Context, name string) (bool, error)
	// ProviderForProject resolves the token provider for a project's GitHub
	// operations: the project's binding when set (fail-closed on a broken
	// one), else the instance provider. Never nil in production — main.go
	// wires it in both auth modes; nil in tests preserves the old
	// fixed-provider path.
	ProviderForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string /*apiBase*/, error)
	// LLMEndpoint is the CM-provisioned inference endpoint attached to every
	// runner/agent trigger payload (single admin-managed key, rotated in one
	// place). nil when llm_endpoint is unset — backends then fall back to
	// their own local config, matching pre-token-authority behavior.
	LLMEndpoint *protocol.LLMEndpoint
}

// EndpointModelView is the api-package projection of modelcatalog.EndpointModel
// used to thread the endpoint model list from main.go through RouterConfig into
// chatHandlers without introducing a modelcatalog→api import cycle.
type EndpointModelView struct {
	ID        string
	Label     string
	MaxTokens int
}

// ServedModelView is the api-package projection of modelcatalog.ServedModel
// (same pattern as EndpointModelView — keeps modelcatalog independent of api).
type ServedModelView struct {
	ID            string
	ContextWindow int
}

// NewRouter creates a new HTTP router with all API routes registered.
// corsOrigin specifies the allowed CORS origin (e.g. "http://localhost:5173").
// If empty, CORS headers are not set.
// Returns http.Handler (wraps mux with metrics and other middleware).
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	// Create handlers
	taskSkillsLister := newTaskSkillsLister(cfg.TaskSkillsDir)
	tsh := &taskSkillHandlers{lister: taskSkillsLister}

	ph := &projectHandlers{
		svc:              cfg.Service,
		runnerEnabled:    cfg.Runner != nil,
		taskSkills:       taskSkillsLister,
		authEnabled:      cfg.AuthService != nil,
		credentialExists: cfg.CredentialExists,
	}
	ch := &cardHandlers{svc: cfg.Service, taskSkills: taskSkillsLister}
	ah := &agentHandlers{svc: cfg.Service}
	acth := &activityHandlers{svc: cfg.Service}
	eh := newEventHandlers(cfg.Bus)
	sh := &syncHandlers{syncer: cfg.Syncer}
	ach := &appConfigHandlers{
		theme:       cfg.Theme,
		version:     cfg.Version,
		taskBackend: cfg.BackendCfg.Name,
		favorites:   extractFavorites(cfg.BackendCfg.Favorites),
		authMode:    cfg.AuthMode,
	}
	bh := &branchHandlers{
		svc:                cfg.Service,
		provider:           cfg.GitHubTokenProvider,
		githubAPIBaseURL:   cfg.GitHubAPIBaseURL,
		allowedHosts:       cfg.GitHubAllowedHosts,
		newBranchClient:    defaultBranchClient,
		providerForProject: cfg.ProviderForProject,
	}

	// Health check
	mux.HandleFunc("GET /healthz", handleHealthz)

	// Readiness check
	rdhz := &readinessHandlers{svc: cfg.Service}

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Service == nil {
			// No card service wired — a router built for narrower coverage
			// (e.g. auth-only or task-skills-only tests) has nothing to
			// report on, so readiness trivially holds rather than panicking
			// on a nil svc inside handleReadyz.
			writeJSON(w, http.StatusOK, readyzResponse{Status: "ok"})

			return
		}

		rdhz.handleReadyz(w, r)
	})

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

	// Project usage, dashboard, and activity feed
	mux.HandleFunc("GET /api/projects/{project}/usage", ph.getProjectUsage)
	mux.HandleFunc("GET /api/projects/{project}/dashboard", ph.getProjectDashboard)
	mux.HandleFunc("POST /api/projects/{project}/recalculate-costs", ph.recalculateCosts)
	mux.HandleFunc("GET /api/projects/{project}/activity", acth.getActivity)

	// Branch listing
	mux.HandleFunc("GET /api/projects/{project}/branches", bh.listBranches)

	// App config
	mux.HandleFunc("GET /api/app/config", ach.getAppConfig)

	// Auth routes — only in multi mode.
	if cfg.AuthService != nil {
		authh := &authHandlers{svc: cfg.AuthService}
		mux.HandleFunc("POST /api/auth/login", authh.login)
		mux.HandleFunc("POST /api/auth/logout", authh.logout)
		mux.HandleFunc("GET /api/auth/session", authh.getSession)
		mux.HandleFunc("GET /api/auth/token/{token}", authh.inspectToken)
		mux.HandleFunc("POST /api/auth/token/{token}", authh.redeemToken)
		mux.HandleFunc("POST /api/auth/password", authh.changePassword)

		adh := &adminHandlers{svc: cfg.AuthService}
		if cfg.Service != nil {
			// Method-value note: cfg.Service.ListProjects on a nil *CardService
			// would still bind to a non-nil func value that panics on call — so
			// this must stay behind the nil check, not become an unconditional
			// assignment.
			adh.listProjectConfigs = cfg.Service.ListProjects
		}

		mux.HandleFunc("GET /api/admin/users", adh.listUsers)
		mux.HandleFunc("POST /api/admin/users", adh.createUser)
		mux.HandleFunc("PATCH /api/admin/users/{username}", adh.patchUser)
		mux.HandleFunc("POST /api/admin/users/{username}/invite", adh.regenerateLink)

		mux.HandleFunc("GET /api/admin/credentials", adh.listCredentials)
		mux.HandleFunc("POST /api/admin/credentials", adh.createCredential)
		mux.HandleFunc("PUT /api/admin/credentials/{name}", adh.putCredential)
		mux.HandleFunc("DELETE /api/admin/credentials/{name}", adh.deleteCredential)
	}

	// Task-skills (used by project default + per-card skill selectors in the UI)
	mux.HandleFunc("GET /api/task-skills", tsh.listTaskSkills)

	// Model catalog for the card pin pickers — available regardless of chat
	// mode; returns source "none" when no catalog builder is configured.
	mch := &modelCatalogHandlers{served: cfg.ServedModels, source: cfg.ServedModelsSource}
	mux.HandleFunc("GET /api/models", mch.listModels)

	// Sync routes
	mux.HandleFunc("POST /api/sync", sh.triggerSync)
	mux.HandleFunc("GET /api/sync", sh.getSyncStatus)

	// Runner routes
	rh := &runnerHandlers{
		svc:                    cfg.Service,
		runner:                 cfg.Runner,
		backendCfg:             cfg.BackendCfg,
		mcpAPIKey:              cfg.MCPAPIKey,
		sessionManager:         cfg.SessionManager,
		replayCache:            runner.NewSignatureCache(),
		catalog:                cfg.Catalog,
		blacklist:              cfg.Blacklist,
		taskSkillsDir:          cfg.TaskSkillsDir,
		taskSkillsGitRemoteURL: cfg.TaskSkillsGitRemoteURL,
		providerForProject:     cfg.ProviderForProject,
		llmEndpoint:            cfg.LLMEndpoint,
		instanceTokenProvider:  cfg.GitHubTokenProvider,
	}
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/run", rh.runCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/stop", rh.stopCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/message", rh.messageCard)
	mux.HandleFunc("POST /api/projects/{project}/cards/{id}/promote", rh.promoteCard)
	mux.HandleFunc("POST /api/projects/{project}/stop-all", rh.stopAll)
	// Backend-callback endpoints mount at /api/<name> derived from the
	// backend entry name. The HMAC key is selected by path at registration
	// time — each handler set closes over exactly one backend's key + replay
	// cache, resolved before any card lookup.
	//
	// GET /api/runner/logs and /api/runner/health are BROWSER-facing (the
	// web UI's EventSource and capacity meter), not backend callbacks —
	// they stay at literal paths. So does the runner-called
	// GET /api/v1/cards/.../autonomous.
	if cfg.Runner != nil {
		// Fail fast at startup: an empty Name would silently mount the
		// backend callbacks at /api/ (derived path would be "/api/"). Real
		// configs can't get here (applyBackendDefaults always sets Name);
		// this guards sloppy test fixtures and future wiring bugs. Same
		// panic-at-registration posture as validateOverrideLimit.
		if cfg.BackendCfg.Name == "" {
			panic("api: RouterConfig.BackendCfg.Name must be set when Runner is non-nil")
		}

		cb := cfg.BackendCfg.CallbackPath()
		mux.HandleFunc("POST "+cb+"/status", rh.runnerStatusUpdate)
		mux.HandleFunc("POST "+cb+"/skill-engaged", rh.handleRunnerSkillEngaged)
		mux.HandleFunc("GET "+cb+"/task-skills-source", rh.getTaskSkillsSource)
		mux.HandleFunc("GET "+cb+"/git-credentials", rh.getGitCredentials)
		mux.HandleFunc("GET /api/runner/logs", rh.streamRunnerLogs)
		mux.HandleFunc("GET /api/runner/health", rh.getRunnerHealth)
		mux.HandleFunc("GET /api/v1/cards/{project}/{id}/autonomous", rh.getCardAutonomous)
	}

	// Chat routes — registered only when both the manager and hub are wired.
	if cfg.ChatManager != nil && cfg.ChatHub != nil {
		chh := newChatHandlers(cfg.ChatManager, cfg.ChatHub, cfg.ChatConfig, cfg.ChatBackendCfg)

		if cfg.ChatEndpointModels != nil {
			emFn := cfg.ChatEndpointModels
			raw := func(ctx context.Context) ([]chatModelEntry, error) {
				views, err := emFn(ctx)
				if err != nil {
					return nil, err
				}

				models := make([]chatModelEntry, len(views))

				for i, v := range views {
					models[i] = chatModelEntry{ID: v.ID, Label: v.Label, MaxTokens: int64(v.MaxTokens)}
				}

				return models, nil
			}
			chh.endpointModels = newCachedEndpointFetcher(raw, endpointModelCacheTTL)
		}

		if cfg.ServedModels != nil {
			smFn := cfg.ServedModels
			chh.servedModels = func(ctx context.Context) []chatModelEntry {
				views := smFn(ctx)
				models := make([]chatModelEntry, len(views))

				for i, v := range views {
					models[i] = chatModelEntry{ID: v.ID, Label: v.ID, MaxTokens: int64(v.ContextWindow)}
				}

				return models
			}
		}

		chh.validateModel = cfg.ValidateChatModel

		mux.HandleFunc("GET /api/chats", chh.listChats)
		mux.HandleFunc("POST /api/chats", chh.createChat)
		mux.HandleFunc("GET /api/chats/{id}", chh.getChat)
		mux.HandleFunc("PATCH /api/chats/{id}", chh.patchChat)
		mux.HandleFunc("DELETE /api/chats/{id}", chh.deleteChat)
		mux.HandleFunc("POST /api/chats/{id}/open", chh.openChat)
		mux.HandleFunc("POST /api/chats/{id}/end", chh.endChat)
		mux.HandleFunc("POST /api/chats/{id}/clear", chh.clearChat)
		mux.HandleFunc("POST /api/chats/{id}/messages", chh.sendMessage)
		mux.HandleFunc("GET /api/chats/{id}/messages", chh.listMessages)
		mux.HandleFunc("GET /api/chats/{id}/stream", chh.streamChat)
		mux.HandleFunc("GET /api/chats/models", chh.listModels)
	}

	// Chat backend callback (HMAC-signed): the dedicated chat service fetches the
	// task-skills git pointer and clones it itself, mirroring the agent. Registered
	// only when a dedicated chat backend key is configured. When the runner serves
	// chat instead (ChatBackendConfig precedence: runner → chat), the chat resolver
	// uses the runner's own /api/runner/task-skills-source, so no /api/chat route is
	// needed here.
	if cfg.ChatBackendCfg.IsEnabled() && cfg.ChatBackendCfg.APIKey != "" {
		cbh := &chatBackendHandlers{
			apiKey:                 cfg.ChatBackendCfg.APIKey,
			replayCache:            runner.NewSignatureCache(),
			taskSkillsDir:          cfg.TaskSkillsDir,
			taskSkillsGitRemoteURL: cfg.TaskSkillsGitRemoteURL,
			instanceTokenProvider:  cfg.GitHubTokenProvider,
		}
		mux.HandleFunc("GET "+cfg.ChatBackendCfg.CallbackPath()+"/task-skills-source", cbh.getTaskSkillsSource)
	}

	// bodyLimitOverrides maps a registered mux pattern (e.g.
	// "POST /api/images") to a per-route body cap. Populated by
	// registerWithBodyLimit so the pattern, handler, and limit are written
	// together in one place — there is no second literal to keep in sync.
	//
	// Invariant: overrides only RAISE the cap above maxRequestBodySize. The
	// short-circuit in bodyLimitN (skipping the mux.Handler walk when
	// ContentLength fits the global cap) relies on this — a smaller override
	// would be silently ignored for declared-length requests. Enforced at
	// registration so a too-small override panics at server startup, before
	// any traffic arrives, with a message pointing at the dependent code.
	bodyLimitOverrides := map[string]int64{}
	registerWithBodyLimit := func(pattern string, limit int64, handler http.Handler) {
		validateOverrideLimit(pattern, limit)
		mux.Handle(pattern, handler)
		bodyLimitOverrides[pattern] = limit
	}

	// Image upload + retrieval. ImageStore is required in production (see
	// RouterConfig.ImageStore); the nil branch keeps tests that don't need
	// image routes from having to wire a SQLite store. The upload route is
	// registered via registerWithBodyLimit so the larger envelope cap and
	// the route literal travel together.
	if cfg.ImageStore != nil {
		ih := newImageHandlers(cfg.ImageStore)
		registerWithBodyLimit("POST /api/images", imageUploadEnvelopeBytes, http.HandlerFunc(ih.upload))
		mux.HandleFunc("GET /api/images/{id}", ih.get)
	}

	// MCP server routes — registered on the inner mux so they share the
	// same middleware chain as every other route (recovery, requestID,
	// observe, bodyLimit, ...).
	if cfg.MCPHandler != nil {
		mux.Handle("POST /mcp", cfg.MCPHandler)
		mux.Handle("GET /mcp", cfg.MCPHandler)
		mux.Handle("DELETE /mcp", cfg.MCPHandler)
	}

	// bodyLimit is built per-router so the override lookup walks this mux's
	// registered patterns via mux.Handler(r) — that lets templated routes opt
	// in to per-route caps in the future without changing the middleware.
	bodyLimit := bodyLimitN(maxRequestBodySize, mux, bodyLimitOverrides)

	// Apply middleware chain. First entry is outermost:
	//   recovery -> securityHeaders -> [cors] -> requestID -> observe -> bodyLimit -> csrfGuard -> mux
	// requestID runs before observe so the request_id is in-context when the
	// request log line fires. observe sits inside recovery so any panic's
	// stack trace is logged with the request's context. csrfGuard sits just
	// outside the mux so route handlers run only when the header check
	// passes (or the path is exempt).
	middlewares := []func(http.Handler) http.Handler{recovery, securityHeaders, requestID, observe, bodyLimit, csrfGuard}
	if cfg.CORSOrigin != "" {
		middlewares = []func(http.Handler) http.Handler{recovery, securityHeaders, corsMiddleware(cfg.CORSOrigin), requestID, observe, bodyLimit, csrfGuard}
	}

	// Session guard sits innermost — just outside the mux — so machine
	// channels and probes are exempted with the same path logic as the CSRF
	// guard, and every gated handler sees the identity context.
	if cfg.AuthService != nil {
		middlewares = append(middlewares, sessionGuard(cfg.AuthService))
	}

	return chain(mux, middlewares...)
}

// csrfGuard rejects browser-initiated cross-origin POST/PUT/PATCH/DELETE
// requests by requiring an X-Requested-With: contextmatrix header on every
// non-safe method. Browsers refuse to set arbitrary custom headers in a
// "simple request"; a CORS preflight is required, and we serve no permissive
// CORS for state-changing routes — so a missing header is a strong signal of
// a cross-origin attempt that bypassed the SOP.
//
// Exempt paths:
//   - GET / HEAD / OPTIONS (read-only)
//   - /api/runner/*, /api/agent/*, /api/chat/* — HMAC-signed backend-callback space; no browser path here
//   - /mcp           — Bearer-authed MCP endpoint
//   - /healthz, /readyz — probe endpoints, no body
//
// The web UI sets the header on every fetch via web/src/api/client.ts.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfExempt(r) {
			next.ServeHTTP(w, r)

			return
		}

		if r.Header.Get("X-Requested-With") != "contextmatrix" {
			writeError(w, http.StatusForbidden, ErrCodeBadRequest,
				"missing X-Requested-With header", "")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// csrfExempt reports whether the request is excluded from the CSRF guard.
// The branches are intentionally narrow — any new state-changing route must
// opt in to the guard, not out.
func csrfExempt(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	path := r.URL.Path

	switch {
	case path == "/healthz" || path == "/readyz":
		return true
	case strings.HasPrefix(path, "/api/runner/"),
		strings.HasPrefix(path, "/api/agent/"),
		strings.HasPrefix(path, "/api/chat/"):
		return true
	case path == "/mcp":
		return true
	}

	return false
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

		// For MCP requests, stash an MCPCall pointer in context so that the
		// inner mcpRequestInfoMiddleware can populate method/tool after parsing
		// the JSON-RPC body. We hold the pointer here so we can read it back
		// after ServeHTTP returns to append mcp_method/mcp_tool to the log line.
		var mcpCall *ctxlog.MCPCall

		if r.URL.Path == "/mcp" {
			var ctx context.Context

			ctx, mcpCall = ctxlog.WithMCPCall(r.Context())
			r = r.WithContext(ctx)
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rw, r)

		dur := time.Since(start)

		attrs := []any{
			"method", r.Method,
			"path", redactPath(r.URL.Path),
			"status", rw.statusCode,
			"duration_ms", dur.Milliseconds(),
		}
		if mcpCall != nil && mcpCall.Method != "" {
			attrs = append(attrs, "mcp_method", mcpCall.Method)
			if mcpCall.Tool != "" {
				attrs = append(attrs, "mcp_tool", mcpCall.Tool)
			}
		}

		ctxlog.Logger(r.Context()).Info("request", attrs...)

		// SSE streams would pollute the REST latency histogram and the
		// path label set — skip them entirely for metrics. MCP Streamable
		// HTTP GET /mcp is a long-lived SSE connection for the same reason.
		// Chat session SSE streams (/api/chats/{id}/stream) follow the same
		// pattern — match by suffix since the id is variable.
		if r.URL.Path == "/api/events" || r.URL.Path == "/api/runner/logs" ||
			(r.Method == http.MethodGet && r.URL.Path == "/mcp") ||
			(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/chats/") && strings.HasSuffix(r.URL.Path, "/stream")) {
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

// redactPath masks path segments that carry secrets. One-time tokens travel
// in the URL (/api/auth/token/<raw>); the raw value must never reach a log
// line. Metrics are unaffected — they label by r.Pattern, not the raw path.
func redactPath(path string) string {
	if strings.HasPrefix(path, "/api/auth/token/") {
		return "/api/auth/token/[redacted]"
	}

	return path
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
					"path", redactPath(r.URL.Path),
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

// validateOverrideLimit panics if a per-route body-limit override is not
// strictly greater than maxRequestBodySize. The short-circuit in bodyLimitN
// skips the override lookup whenever ContentLength fits the global cap, so a
// smaller override would be silently ignored for declared-length requests.
// Fails closed at server startup, before any traffic arrives.
func validateOverrideLimit(pattern string, limit int64) {
	if limit <= maxRequestBodySize {
		panic(fmt.Sprintf(
			"bodyLimitOverrides[%q] = %d must be greater than global cap %d "+
				"(short-circuit in bodyLimitN assumes overrides only raise the cap)",
			pattern, limit, maxRequestBodySize,
		))
	}
}

// bodyLimitN returns a middleware that caps request body size to maxBytes.
// Requests whose Content-Length exceeds the limit are rejected immediately with 413.
// For streaming requests without Content-Length, http.MaxBytesReader enforces the
// limit when the body is read; bodyLimitN wraps the ResponseWriter to intercept the
// first write after a body-too-large error and ensure a 413 status is sent.
//
// overrides maps a registered mux pattern (e.g. "POST /api/images") to a
// per-route cap. The pattern is recovered from mux.Handler(r), which returns
// the pattern that would dispatch the request — so templated routes opt in
// by registering with the same pattern they use on the mux. Requests that do
// not match any registered pattern fall through to maxBytes.
func bodyLimitN(maxBytes int64, mux *http.ServeMux, overrides map[string]int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := maxBytes

			// Skip the O(n_routes) mux.Handler(r) pattern walk when no
			// override could possibly raise the cap: either there are no
			// overrides at all, or the request advertises a Content-Length
			// that already fits in the global cap (overrides only raise
			// it). Streaming requests (ContentLength < 0) must still walk
			// the mux so a route override can apply via MaxBytesReader.
			needOverrideLookup := len(overrides) > 0 && (r.ContentLength < 0 || r.ContentLength > maxBytes)
			if mux != nil && needOverrideLookup {
				if _, pattern := mux.Handler(r); pattern != "" {
					if override, ok := overrides[pattern]; ok {
						limit = override
					}
				}
			}

			// Reject immediately when Content-Length is declared and over limit.
			if r.ContentLength > limit {
				writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge, "request body too large", "")

				return
			}

			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}

			next.ServeHTTP(w, r)
		})
	}
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
	case errors.Is(err, service.ErrPromoteRequiresHuman):
		writeError(w, http.StatusForbidden, ErrCodeHumanOnlyField,
			"promote requires a human agent", "agent_id must start with \"human:\"")

	// --- Bad-request sentinels (400) ---
	case errors.Is(err, storage.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid path", sanitizeErrorDetails(err))
	case errors.Is(err, storage.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid input", sanitizeErrorDetails(err))

	// --- Validation sentinels (422) — mutation body shape/semantics ---
	case errors.Is(err, board.ErrInvalidProjectConfig),
		errors.Is(err, board.ErrMissingStalledState),
		errors.Is(err, board.ErrMissingStalledTransitions),
		errors.Is(err, board.ErrMissingNotPlannedState),
		errors.Is(err, board.ErrMissingNotPlannedTransitions):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid project config", sanitizeErrorDetails(err))
	case errors.Is(err, board.ErrInvalidType), errors.Is(err, board.ErrInvalidState), errors.Is(err, board.ErrInvalidPriority),
		errors.Is(err, board.ErrInvalidAutonomousConfig),
		errors.Is(err, board.ErrInvalidExternalURL), errors.Is(err, board.ErrInvalidRunnerStatus),
		errors.Is(err, board.ErrInvalidPhase):
		var ve *board.ValidationError

		details := ""
		if errors.As(err, &ve) {
			details = ve.Error()
		}

		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "validation error", details)
	case errors.Is(err, service.ErrInvalidPRUrl):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid PR URL", sanitizeErrorDetails(err))
	case errors.Is(err, service.ErrInvalidModelPin):
		writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "model pin not in catalog", sanitizeErrorDetails(err))
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
