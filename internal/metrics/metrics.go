package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// All metrics use the `contextmatrix_` prefix to keep Grafana / Loki queries
// uniformly namespaced. When adding a new collector, set its `Name` to start
// with `contextmatrix_` and add it to the list in Register.
var (
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_http_requests_total",
		Help: "Total HTTP requests by method, path, and status.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "contextmatrix_http_request_duration_seconds",
		Help:    "HTTP request latency by method and path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	SSEActiveConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "contextmatrix_sse_active_connections",
		Help: "Active SSE connections across all streaming endpoints.",
	})

	EventBusDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_eventbus_dropped_total",
		Help: "Event bus messages dropped due to full subscriber channels.",
	})

	// BackendWebhookTotal counts outbound task-backend webhook calls by outcome.
	// The result label is bounded to {success, failure}.
	BackendWebhookTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_backend_webhook_total",
		Help: "Outbound task backend webhook calls labeled by result.",
	}, []string{"result"})

	// GitSyncDuration uses wider buckets than DefBuckets because repo
	// commits routinely exceed 10s when pushing to a remote or staging
	// a large change set.
	GitSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "contextmatrix_gitsync_duration_seconds",
		Help:    "Duration of git commit operations.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 30, 60},
	})

	StallScanDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "contextmatrix_stall_scan_duration_seconds",
		Help:    "Duration of heartbeat stall scanner ticks.",
		Buckets: prometheus.DefBuckets,
	})

	StallCardsMarked = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_stall_cards_marked_total",
		Help: "Cards transitioned to stalled by the heartbeat scanner.",
	})

	// CardCacheSize tracks the total number of cards resident in the
	// FilesystemStore in-memory card cache across all projects.
	CardCacheSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "contextmatrix_card_cache_size",
		Help: "Total cards currently held in the in-memory card cache.",
	})

	// CardCacheMissTotal counts GetCard requests that missed the cache and
	// fell back to disk. Under normal operation this should be near zero;
	// elevated values suggest cache invalidation or a race during reload.
	CardCacheMissTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_card_cache_miss_total",
		Help: "GetCard requests that missed the in-memory cache and read from disk.",
	})

	// CommitQueueDepth tracks buffered (not yet picked up) commit jobs across
	// all per-project worker goroutines. A sustained non-zero value indicates
	// commits are arriving faster than go-git can service them.
	CommitQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "contextmatrix_commit_queue_depth",
		Help: "Buffered commit jobs awaiting a worker.",
	})

	// CommitDuration records how long each commit takes once a worker picks
	// it up. Distinct from GitSyncDuration (which is still observed inside
	// Manager) so dashboards can distinguish queue wait time from commit time.
	CommitDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "contextmatrix_commit_duration_seconds",
		Help:    "Duration of an individual commit operation executed by the commit queue.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 30, 60},
	})

	// CommitErrorsTotal counts commit failures returned by the queue worker.
	CommitErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_commit_errors_total",
		Help: "Commit failures reported by the commit queue worker.",
	})

	// CommitCancellationsTotal counts commit jobs that did not execute because
	// the caller's context was cancelled before the worker could start the
	// commit. Distinct from CommitErrorsTotal: a cancellation is not a commit
	// failure, but it is observable so dashboards can distinguish "queue
	// drained on shutdown" from "queue had no traffic".
	CommitCancellationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_commit_cancellations_total",
		Help: "Commit jobs skipped because the caller's context was cancelled before execution.",
	})

	// ParentAutoTransitionErrors counts failures during parent auto-transition
	// commits. Auto-transitions are best-effort fire-and-forget from the child
	// write path; operators should alert on sustained non-zero values since
	// each failed transition leaves the parent desynchronised from its subtask
	// state until a subsequent mutation re-commits it.
	ParentAutoTransitionErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_parent_autotransition_errors_total",
		Help: "Parent auto-transition commit failures (best-effort; fire-and-forget).",
	})

	// RollbackFailuresTotal counts the rare double-failure case where a
	// commit fails AND the subsequent cache+disk rollback also fails. When
	// non-zero, the cache and on-disk state for the named card are
	// inconsistent until manual intervention. Operators should alert on
	// any non-zero value - every increment is a data-integrity event.
	RollbackFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_rollback_failures_total",
		Help: "Card-mutation rollback failures after a commit failure (cache + disk left inconsistent).",
	})

	// ReportUsageUnknownModelTotal counts report_usage calls where the model
	// name is not in the configured token_costs map. Each increment means an
	// agent reported tokens for a model we cannot price, so cost will be $0
	// for that delta. The model label lets operators alert per-model.
	ReportUsageUnknownModelTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_report_usage_unknown_model_total",
		Help: "report_usage calls where the model is not in the token_costs map (cost not calculated).",
	}, []string{"model"})

	// ChatUsageUnknownModelTotal counts handleUsageEntry calls where the model
	// name resolved from the usage frame is not in the configured token_costs
	// map. Cost is persisted as $0 for that frame; tokens still accumulate.
	// The model label lets operators detect deprecated or misconfigured model
	// names in active chat sessions.
	ChatUsageUnknownModelTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_chat_usage_unknown_model_total",
		Help: "chat usage frames where the model is not in the token_costs map (cost recorded as $0).",
	}, []string{"model"})

	// GitHubPagesTruncatedTotal counts FetchOpenIssues / FetchBranches calls
	// that hit the maxPages safety cap and silently dropped remaining pages.
	// The resource label is one of {issues, branches}.
	GitHubPagesTruncatedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_github_pages_truncated_total",
		Help: "GitHub API paginated fetches that hit the maxPages cap and dropped remaining pages.",
	}, []string{"resource"})

	// ChatCostSummaryErrorsTotal counts GetChatCostSummary failures inside
	// GetDashboard. Each increment means the chat-cost fields on the dashboard
	// payload fell back to zero for that request. Operators should alert on a
	// sustained non-zero rate as it indicates a broken chat store or schema mismatch.
	ChatCostSummaryErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "contextmatrix_chat_cost_summary_errors_total",
		Help: "GetChatCostSummary failures during GetDashboard (chat-cost fields fall back to zero).",
	})

	// Runtime execution telemetry. All label values pass through server-side
	// normalization (NormalizePhase / NormalizeStep, run-mode and outcome enums
	// computed in the service layer), so clients cannot mint unbounded series.
	// The model label is verbatim, matching ReportUsageUnknownModelTotal.

	LLMCostUSDTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_llm_cost_usd_total",
		Help: "LLM spend in USD reported via report_usage, by project, model, phase, run mode, and cost source.",
	}, []string{"project", "model", "phase", "run_mode", "source"})

	LLMTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_llm_tokens_total",
		Help: "LLM tokens reported via report_usage, by project, model, phase, and token kind.",
	}, []string{"project", "model", "phase", "kind"})

	LLMCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_llm_calls_total",
		Help: "report_usage calls carrying a model, by project, model, phase, and run mode.",
	}, []string{"project", "model", "phase", "run_mode"})

	// LLMStepDuration buckets span a single harness step: a multi-turn agentic
	// model call that runs seconds to tens of minutes.
	LLMStepDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "contextmatrix_llm_step_duration_seconds",
		Help:    "Wall time of one harness model step as reported by the agent, by model, phase, and step kind.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600},
	}, []string{"model", "phase", "step"})

	// PhaseDuration is observed server-side from consecutive card phase
	// updates. Phases run from sub-minute (gate-ish) to many hours (execute).
	PhaseDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "contextmatrix_phase_duration_seconds",
		Help:    "Time a card spent in an FSM phase, measured between phase updates, by project and phase.",
		Buckets: []float64{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200, 14400, 28800},
	}, []string{"project", "phase"})

	// CardRunDuration measures claim to terminal (release/stall/failed
	// callback) for parent and standalone cards; subtask claims are excluded.
	CardRunDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "contextmatrix_card_run_duration_seconds",
		Help:    "Card run duration from claim to terminal event, by project, outcome, and run mode.",
		Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 28800, 57600, 86400},
	}, []string{"project", "outcome", "run_mode"})

	CardRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_card_runs_total",
		Help: "Card runs reaching a terminal event, by project, outcome, and run mode.",
	}, []string{"project", "outcome", "run_mode"})

	RunAgents = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "contextmatrix_run_agents",
		Help:    "Agents participating in a card run (mob participants + guests, best-of-N candidates, or 1), by run mode.",
		Buckets: []float64{1, 2, 3, 4, 5, 6, 8, 10},
	}, []string{"run_mode"})

	ModelOutcomesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_model_outcomes_total",
		Help: "Best-of-N per-candidate judge outcomes recorded via report_model_outcome, by model and result.",
	}, []string{"model", "result"})

	ModelBlacklistsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_model_blacklists_total",
		Help: "Models reported incapable via report_incapable_model, by model.",
	}, []string{"model"})

	ChatCostUSDTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_chat_cost_usd_total",
		Help: "Chat session spend in USD priced from worker usage frames, by project and model.",
	}, []string{"project", "model"})

	ChatTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "contextmatrix_chat_tokens_total",
		Help: "Chat session tokens from worker usage frames, by project, model, and token kind.",
	}, []string{"project", "model", "kind"})
)

// validPhases mirrors the board package's phase enum. Duplicated here rather
// than imported so the metrics package stays dependency-free and usable from
// any layer without import cycles.
var validPhases = map[string]struct{}{
	"plan": {}, "execute": {}, "judge": {}, "document": {},
	"review": {}, "integrate": {}, "done": {},
}

var validSteps = map[string]struct{}{
	"main": {}, "gate": {}, "brainstorm": {}, "verify_propose": {},
	"mob_seat": {}, "mob_moderator": {}, "checkpoint": {}, "judge": {},
}

// NormalizePhase maps a client-supplied phase to the bounded label enum:
// empty becomes "none", unknown values become "other".
func NormalizePhase(phase string) string {
	if phase == "" {
		return "none"
	}

	if _, ok := validPhases[phase]; ok {
		return phase
	}

	return "other"
}

// NormalizeStep maps a client-supplied step to the bounded label enum:
// empty becomes "main" (the primary phase model call), unknown values
// become "other".
func NormalizeStep(step string) string {
	if step == "" {
		return "main"
	}

	if _, ok := validSteps[step]; ok {
		return step
	}

	return "other"
}

// Register registers all metrics with the given registerer. Re-registering an
// already-registered collector is not an error; tests and potential hot-reload
// paths can call Register more than once without panicking.
func Register(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		HTTPRequestsTotal,
		HTTPRequestDuration,
		SSEActiveConnections,
		EventBusDropped,
		BackendWebhookTotal,
		GitSyncDuration,
		StallScanDuration,
		StallCardsMarked,
		CardCacheSize,
		CardCacheMissTotal,
		CommitQueueDepth,
		CommitDuration,
		CommitErrorsTotal,
		CommitCancellationsTotal,
		ParentAutoTransitionErrors,
		RollbackFailuresTotal,
		ReportUsageUnknownModelTotal,
		GitHubPagesTruncatedTotal,
		ChatUsageUnknownModelTotal,
		ChatCostSummaryErrorsTotal,
		LLMCostUSDTotal,
		LLMTokensTotal,
		LLMCallsTotal,
		LLMStepDuration,
		PhaseDuration,
		CardRunDuration,
		CardRunsTotal,
		RunAgents,
		ModelOutcomesTotal,
		ModelBlacklistsTotal,
		ChatCostUSDTotal,
		ChatTokensTotal,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if !errors.As(err, &are) {
				panic(err)
			}
		}
	}
}
