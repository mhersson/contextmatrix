package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/service"
)

// runnerSubsystems groups the optional task-backend client, the session-log
// manager, and any related coordinators so they can be wired in one place.
type runnerSubsystems struct {
	// Client is nil when no task backend is configured (no enabled runner/agent entry).
	Client     *runner.Client
	SessionLog *sessionlog.Manager
}

// wireRunnerSubsystems constructs the task-backend client, starts the
// end-session subscriber, creates the session-log manager, starts its idle
// sweeper, and (when enabled) launches the Docker-authoritative reconciliation
// sweep. Returns the aggregate and a cleanup closure the caller must defer.
func wireRunnerSubsystems(
	ctx context.Context,
	cfg *config.Config,
	svc *service.CardService,
	bus *events.Bus,
) (*runnerSubsystems, func()) {
	sys := &runnerSubsystems{}

	taskCfg, taskEnabled := cfg.TaskBackendConfig()

	// --- task backend client (optional) ---
	if taskEnabled {
		sys.Client = runner.NewClient(taskCfg.URL, taskCfg.APIKey)
		slog.Info("task backend enabled", "name", taskCfg.Name, "url", taskCfg.URL)

		runner.StartEndSessionSubscriber(ctx, bus, svc, sys.Client, slog.Default())
		slog.Info("end-session subscriber started")
	}

	// --- reconciliation sweep (task backend required; it is the agent
	// backend's only reconcile mechanism) ---
	if taskEnabled {
		reconcileInterval := taskCfg.ReconcileIntervalDuration()
		runner.StartReconciliationSweep(
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
	// taskCfg is the zero value when disabled — an empty URL.
	sys.SessionLog = sessionlog.NewManager(
		sessionlog.WithRunnerConfig(taskCfg.URL, taskCfg.APIKey),
		sessionlog.WithMaxSessions(64),
		sessionlog.WithSessionTTL(2*time.Hour),
	)
	sys.SessionLog.StartSweeper(ctx)
	svc.SetSessionManager(sys.SessionLog)
	slog.Info("session log manager initialized")

	return sys, func() {}
}
