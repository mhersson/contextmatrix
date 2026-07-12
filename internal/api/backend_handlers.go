package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/service"
)

// Error codes for task-backend errors. BACKEND_* codes describe the backend
// service (missing or unreachable); WORKER_* codes describe a card's worker
// (already running, or not running when one is required).
//
// Conflict (409) and unavailable (502) are split so callers can tell an
// already-running card from an unreachable backend host.
const (
	ErrCodeBackendDisabled    = "BACKEND_DISABLED"
	ErrCodeWorkerConflict     = "WORKER_CONFLICT"
	ErrCodeBackendUnavailable = "BACKEND_UNAVAILABLE"
	ErrCodeWorkerNotRunning   = "WORKER_NOT_RUNNING"
)

// catalogProvider supplies the current auto-selectable model candidates.
// Implemented by modelcatalog.Builder; the interface lives here so the api
// package does not import modelcatalog.
type catalogProvider interface {
	Candidates(ctx context.Context) []protocol.CandidateModel
}

// blacklistReader returns the set of OpenRouter slugs that must never be
// auto-selected. Implemented by opstore/sqlite.Store; lives here for the same
// reason as catalogProvider.
type blacklistReader interface {
	BlacklistedSlugs(ctx context.Context) ([]string, error)
}

// outcomeStatsReader supplies Best-of-N head-to-head aggregates for
// selection. Implemented by opstore/sqlite.Store; lives here for the same
// reason as catalogProvider and blacklistReader.
type outcomeStatsReader interface {
	ModelOutcomeStats(ctx context.Context) ([]sqlite.OutcomeStats, error)
}

// backendHandlers contains handlers for remote execution endpoints. This
// handler set exists only for the agent backend — the callback endpoints it
// serves mount at config.AgentCallbackPath.
type backendHandlers struct {
	svc               *service.CardService
	backend           TaskBackend                // nil when no task backend is configured
	backendCfg        *config.AgentBackendConfig // resolved agent entry; never nil (NewRouter normalizes an absent entry to the zero value)
	mcpAPIKey         string
	sessionManager    *sessionlog.Manager // nil when session manager is not configured
	keepaliveInterval time.Duration       // zero → use default (30s)

	taskSkillsDir          string
	taskSkillsGitRemoteURL string

	// catalog and blacklist supply model-selection inputs for agent-backend
	// triggers. Both are nil until T8 wires the real implementations in main.go;
	// runCard guards on catalog != nil before attaching Selection.
	catalog   catalogProvider
	blacklist blacklistReader

	// outcomes supplies Best-of-N head-to-head aggregates attached per-candidate
	// to SelectionContext.Candidates[i].Outcomes. nil disables attachment; a
	// read failure is best-effort (logged, selection proceeds without stats),
	// mirroring the blacklist read above.
	outcomes outcomeStatsReader

	// bestOfN bounds the card-level best_of_n value forwarded to the agent
	// backend (payload.BestOfN = min(card.BestOfN, bestOfN.MaxCandidates)) and
	// supplies SelectionContext.OutcomeFloor. Zero value (MaxCandidates 0)
	// clamps every non-zero card value down to 0, matching RouterConfig.BestOfN's
	// pre-config.Load-defaults zero-value contract.
	bestOfN config.BestOfNConfig

	// mob supplies the trigger-time mob session re-clamp bounds and the guest
	// registry. The stored card values were validated against the config in
	// effect at WRITE time; the trigger clamp against the CURRENT config is
	// authoritative. Zero value (MaxParticipants 0) clamps everything off,
	// matching bestOfN's zero-value contract.
	mob config.MobConfig

	// replayCache guards the backend-callback authentication path against
	// replayed HMAC signatures. Populated at construction time; non-nil
	// whenever a backend API key is configured.
	replayCache *backend.SignatureCache

	// providerForProject resolves the project-scoped git-token provider used
	// to mint the trigger payload's GitToken: the project's credential
	// binding when set (fail-closed on a broken one), else the instance
	// provider. nil preserves pre-token-authority behavior — no GitToken is
	// attached and the trigger is never rejected on its account.
	providerForProject func(ctx context.Context, project string) (githubauth.TokenGenerator, string, error)

	// llmEndpoint is the CM-provisioned inference endpoint attached to every
	// trigger payload. nil when llm_endpoint is unconfigured — backends then
	// fall back to their own local config.
	llmEndpoint *protocol.LLMEndpoint

	// instanceTokenProvider mints the instance-scoped git credential attached
	// to task-skills-source responses. Unlike providerForProject (per-project,
	// fail-closed on a broken binding), this is the flat instance provider —
	// task-skills is never project-scoped, so there is no binding to fail
	// closed on. nil disables the token fields (pre-token-authority behavior).
	instanceTokenProvider githubauth.TokenGenerator

	// healthCache memoises /api/backend/health responses so concurrent browser
	// tabs don't each fire a fresh probe at the backend — and so a backend
	// outage doesn't cause every refresh to block for the full per-request
	// timeout.
	healthCache healthProbeCache
}

// healthProbeCache is a small TTL cache for backend /health responses.
// Both successes and failures are cached so an outage doesn't produce a
// thundering-herd of blocked goroutines hitting a dead backend.
//
// Concurrent callers arriving during a cold-or-expired window are
// coalesced via singleflight, so exactly one upstream probe runs per
// TTL window regardless of inbound concurrency. The probe itself uses
// a detached context so a single caller cancelling mid-probe cannot
// poison the cache for the rest of the TTL window.
type healthProbeCache struct {
	mu      sync.Mutex
	expires time.Time
	info    backend.HealthInfo
	err     error

	flight singleflight.Group
}

// backendHealthCacheTTL is how long a /api/backend/health probe result is
// reused.
// Short enough that operators see capacity changes promptly, long enough to
// dampen multi-tab refresh storms.
const backendHealthCacheTTL = 2 * time.Second

// backendHealthProbeTimeout is the per-probe timeout for upstream /health
// calls. Tighter than the backend client's default 10s so a hung backend
// doesn't pin every browser tab for that long.
const backendHealthProbeTimeout = 3 * time.Second

// isRemoteExecutionEnabled checks if remote execution is enabled for the given project,
// falling back to whether a task backend is configured when not set per-project.
func (h *backendHandlers) isRemoteExecutionEnabled(r *http.Request, project string) bool {
	projectCfg, err := h.svc.GetProject(r.Context(), project)
	if err != nil {
		return h.backend != nil
	}

	if projectCfg.RemoteExecution != nil && projectCfg.RemoteExecution.Enabled != nil {
		return *projectCfg.RemoteExecution.Enabled
	}

	return h.backend != nil
}
