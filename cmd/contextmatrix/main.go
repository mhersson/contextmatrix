package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	ghimport "github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/lock"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/runner"
	"github.com/mhersson/contextmatrix/internal/runner/sessionlog"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/mhersson/contextmatrix/web"
)

var (
	version   string
	gitCommit string
	buildTime string
)

func buildVersion() string {
	if buildTime == "" {
		return ""
	}

	if version != "" {
		return buildTime + " " + version
	}

	if gitCommit != "" {
		return buildTime + " " + gitCommit
	}

	return buildTime
}

func main() {
	configPath := flag.String("config", "", "path to config file")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if *configPath == "" {
		resolved := config.FindConfigPath()
		configPath = &resolved
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("config loaded", "path", *configPath)
	slog.Info("boards git auth", "mode", cfg.Boards.GitAuthMode)

	// Parse heartbeat timeout
	heartbeatTimeout, err := cfg.HeartbeatDuration()
	if err != nil {
		slog.Error("invalid heartbeat_timeout", "error", err)
		os.Exit(1)
	}

	// Initialize git manager (boards directory IS the git repo).
	// Must run before storage so clone-on-empty can populate the directory.
	cloneURL := ""
	if cfg.Boards.GitCloneOnEmpty {
		cloneURL = cfg.Boards.GitRemoteURL
	}

	git, err := gitops.NewManager(cfg.Boards.Dir, cloneURL, cfg.Boards.GitAuthMode, cfg.GitHub.Token)
	if err != nil {
		slog.Error("failed to create git manager", "error", err)
		os.Exit(1)
	}

	slog.Info("git manager initialized", "repo_path", cfg.Boards.Dir)

	// Initialize storage
	store, err := storage.NewFilesystemStore(cfg.Boards.Dir)
	if err != nil {
		slog.Error("failed to create storage", "error", err)
		os.Exit(1)
	}

	slog.Info("storage initialized", "boards_dir", cfg.Boards.Dir)

	// Initialize event bus
	bus := events.NewBus()

	slog.Info("event bus initialized")

	// Initialize lock manager
	lockMgr := lock.NewManager(store, heartbeatTimeout)
	slog.Info("lock manager initialized", "timeout", heartbeatTimeout)

	// Convert token costs from config to service types
	var tokenCosts map[string]service.ModelCost
	if cfg.TokenCosts != nil {
		tokenCosts = make(map[string]service.ModelCost, len(cfg.TokenCosts))
		for model, cost := range cfg.TokenCosts {
			tokenCosts[model] = service.ModelCost{Prompt: cost.Prompt, Completion: cost.Completion}
		}
	}

	// Initialize card service
	svc := service.NewCardService(store, git, lockMgr, bus, cfg.Boards.Dir, tokenCosts, cfg.Boards.GitAutoCommit, cfg.Boards.GitDeferredCommit)

	slog.Info("card service initialized")

	// Create context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start timeout checker (checks every minute)
	svc.StartTimeoutChecker(ctx, time.Minute)

	// Initialize git sync
	var syncer *gitsync.Syncer

	if git.HasRemote() {
		pullInterval, _ := cfg.PullIntervalDuration()

		syncer = gitsync.NewSyncer(git, store, svc, bus, cfg.Boards.Dir,
			cfg.Boards.GitAutoPull, cfg.Boards.GitAutoPush, pullInterval, cfg.Boards.GitAuthMode, cfg.GitHub.Token)
		if syncer != nil {
			if err := syncer.PullOnStartup(ctx); err != nil {
				slog.Warn("initial pull failed", "error", err)
			}

			if cfg.Boards.GitAutoPush {
				svc.SetOnCommit(syncer.NotifyCommit)
			}

			syncer.Start(ctx)
			slog.Info("git sync initialized",
				"auto_pull", cfg.Boards.GitAutoPull,
				"auto_push", cfg.Boards.GitAutoPush,
				"pull_interval", pullInterval,
			)
		}
	}

	// Start GitHub issue syncer if configured
	var ghSyncer *ghimport.Syncer

	if cfg.GitHub.IssueImporting.Enabled {
		syncInterval, _ := cfg.GitHub.IssueImporting.SyncIntervalDuration()
		ghClient := ghimport.NewClientWithBaseURL(cfg.GitHub.Token, cfg.GitHub.ResolvedAPIBaseURL())
		ghSyncer = ghimport.NewSyncer(svc, store, ghClient, cfg.Boards.Dir, syncInterval, cfg.GitHub.AllowedHosts())
		ghSyncer.Start(ctx)
		slog.Info("github issue sync enabled", "interval", syncInterval)
	}

	// Create runner client if enabled
	var runnerClient *runner.Client
	if cfg.Runner.Enabled {
		runnerClient = runner.NewClient(cfg.Runner.URL, cfg.Runner.APIKey)
		slog.Info("runner integration enabled", "url", cfg.Runner.URL)

		runner.StartEndSessionSubscriber(ctx, bus, svc, runnerClient, slog.Default())
		slog.Info("end-session subscriber started")
	}

	// Create session log manager and start its idle sweeper.
	// The manager is always constructed so the card-scoped SSE path is available
	// even when the runner is disabled (Subscribe returns empty snapshots).
	sessionMgr := sessionlog.NewManager(
		sessionlog.WithRunnerConfig(cfg.Runner.URL, cfg.Runner.APIKey),
		sessionlog.WithMaxSessions(64),
		sessionlog.WithSessionTTL(2*time.Hour),
	)
	sessionMgr.StartSweeper(ctx)
	svc.SetSessionManager(sessionMgr)
	slog.Info("session log manager initialized")

	// Create router with all API routes
	mux := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Bus:                bus,
		CORSOrigin:         cfg.CORSOrigin,
		Syncer:             syncer,
		Runner:             runnerClient,
		RunnerCfg:          cfg.Runner,
		MCPAPIKey:          cfg.MCPAPIKey,
		Port:               cfg.Port,
		GitHubToken:        cfg.GitHub.Token,
		GitHubAPIBaseURL:   cfg.GitHub.ResolvedAPIBaseURL(),
		GitHubAllowedHosts: cfg.GitHub.AllowedHosts(),
		SessionManager:     sessionMgr,
		Theme:              cfg.Theme,
		Version:            buildVersion(),
	})

	// Create MCP server and register on the mux
	mcpSrv := mcpserver.NewServer(svc, cfg.SkillsDir)

	mcpHandler := mcpserver.NewHandler(mcpSrv, cfg.MCPAPIKey)
	if cfg.MCPAPIKey != "" {
		slog.Info("MCP authentication enabled")
	}

	wrappedMCPHandler := api.WrapMCPHandler(mcpHandler)
	mux.Handle("POST /mcp", wrappedMCPHandler)
	mux.Handle("GET /mcp", wrappedMCPHandler)
	mux.Handle("DELETE /mcp", wrappedMCPHandler)
	slog.Info("MCP server registered", "endpoint", "/mcp")

	// Embed frontend and create SPA handler
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		slog.Error("failed to create dist filesystem", "error", err)
		cancel()
		os.Exit(1) //nolint:gocritic // cancel called explicitly above
	}

	handler := newSPAHandler(mux, distFS)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		slog.Info("starting server", "port", cfg.Port)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)

			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
	case err := <-errCh:
		slog.Error("server failed, initiating shutdown", "error", err)
	}

	slog.Info("shutting down server")

	// Stop background tasks
	cancel()

	// Wait for background goroutines to finish
	if syncer != nil {
		syncer.Wait()
	}

	if ghSyncer != nil {
		ghSyncer.Wait()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		shutdownCancel()
		os.Exit(1)
	}

	slog.Info("server stopped")
}

// newSPAHandler wraps the API handler with static file serving and SPA fallback.
// API and health check routes are forwarded to the API handler. All other requests
// are served from the embedded frontend filesystem, falling back to index.html
// for client-side routing.
func newSPAHandler(apiHandler http.Handler, fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/mcp" {
			apiHandler.ServeHTTP(w, r)

			return
		}

		// Try to serve a static file from the embedded dist
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(fsys, path); err == nil {
			fileServer.ServeHTTP(w, r)

			return
		}

		// SPA fallback: serve index.html for client-side routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
