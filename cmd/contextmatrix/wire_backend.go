package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/mhersson/contextmatrix/internal/backend"
	"github.com/mhersson/contextmatrix/internal/backend/sessionlog"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/service"
)

// backendSubsystems groups the optional task-backend client, the session-log
// manager, and any related coordinators so they can be wired in one place.
type backendSubsystems struct {
	// Client is nil when no task backend is configured (no enabled agent entry).
	Client     *backend.Client
	SessionLog *sessionlog.Manager
}

// wireBackendSubsystems constructs the task-backend client, starts the
// end-session subscriber, creates the session-log manager, starts its idle
// sweeper, and (when enabled) launches the Docker-authoritative reconciliation
// sweep. Teardown is owned by the shutdown sequence (wire_shutdown.go), which
// closes the session-log manager.
func wireBackendSubsystems(
	ctx context.Context,
	cfg *config.Config,
	svc *service.CardService,
	bus *events.Bus,
) *backendSubsystems {
	sys := &backendSubsystems{}

	taskCfg, taskEnabled := cfg.AgentBackend()

	// taskURL/taskAPIKey stay empty when no agent backend is enabled - the
	// session-log manager below is always constructed and treats an empty
	// URL as "disabled".
	var taskURL, taskAPIKey string

	if taskEnabled {
		taskURL, taskAPIKey = taskCfg.URL, taskCfg.APIKey
	}

	// --- task backend client (optional) ---
	if taskEnabled {
		sys.Client = backend.NewClient(taskCfg.URL, taskCfg.APIKey)
		slog.Info("task backend enabled", "name", config.BackendNameAgent, "url", taskCfg.URL)

		backend.StartEndSessionSubscriber(ctx, bus, svc, sys.Client, slog.Default())
		slog.Info("end-session subscriber started")
	}

	// --- reconciliation sweep (task backend required; it is the agent
	// backend's only reconcile mechanism) ---
	if taskEnabled {
		reconcileInterval := taskCfg.ReconcileIntervalDuration()
		backend.StartReconciliationSweep(
			ctx, svc,
			sys.Client,
			reconcileInterval,
			slog.Default(),
		)

		if reconcileInterval > 0 {
			slog.Info("reconciliation sweep started",
				"interval", reconcileInterval,
			)
		}
	}

	// --- session-log manager (always constructed; Subscribe is a no-op when disabled) ---
	sys.SessionLog = sessionlog.NewManager(
		sessionlog.WithBackendConfig(taskURL, taskAPIKey),
		sessionlog.WithMaxSessions(64),
		sessionlog.WithSessionTTL(2*time.Hour),
	)
	sys.SessionLog.StartSweeper(ctx)
	svc.SetSessionManager(sys.SessionLog)
	slog.Info("session log manager initialized")

	return sys
}
