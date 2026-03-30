package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Parse heartbeat timeout
	heartbeatTimeout, err := cfg.HeartbeatDuration()
	if err != nil {
		slog.Error("invalid heartbeat_timeout", "error", err)
		os.Exit(1)
	}

	// Initialize storage
	store, err := storage.NewFilesystemStore(cfg.BoardsDir)
	if err != nil {
		slog.Error("failed to create storage", "error", err)
		os.Exit(1)
	}
	slog.Info("storage initialized", "boards_dir", cfg.BoardsDir)

	// Initialize git manager (boards directory IS the git repo)
	git, err := gitops.NewManager(cfg.BoardsDir)
	if err != nil {
		slog.Error("failed to create git manager", "error", err)
		os.Exit(1)
	}
	slog.Info("git manager initialized", "repo_path", cfg.BoardsDir)

	// Initialize event bus
	bus := events.NewBus()
	slog.Info("event bus initialized")

	// Initialize lock manager
	lockMgr := lock.NewManager(store, heartbeatTimeout)
	slog.Info("lock manager initialized", "timeout", heartbeatTimeout)

	// Initialize card service
	svc := service.NewCardService(store, git, lockMgr, bus, cfg.BoardsDir)
	slog.Info("card service initialized")

	// Create context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start timeout checker (checks every minute)
	svc.StartTimeoutChecker(ctx, time.Minute)

	// Create router with all API routes
	mux := api.NewRouter(svc, bus)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	// Stop background tasks
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
