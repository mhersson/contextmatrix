package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mhersson/contextmatrix/internal/api"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	ghimport "github.com/mhersson/contextmatrix/internal/github"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/jira"
	"github.com/mhersson/contextmatrix/internal/gitsync"
	"github.com/mhersson/contextmatrix/internal/lock"
	mcpserver "github.com/mhersson/contextmatrix/internal/mcp"
	"github.com/mhersson/contextmatrix/internal/metrics"
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

	if *configPath == "" {
		resolved := config.FindConfigPath()
		configPath = &resolved
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// Use a plain text handler for the startup error since config isn't loaded yet.
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Register Prometheus metrics early, before building any component that
	// may observe them.
	metrics.Register(prometheus.DefaultRegisterer)

	logger := slog.New(cfg.BuildSlogHandler(os.Stdout))
	slog.SetDefault(logger)

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

	// Initialize the per-project commit queue so writes do not serialize on
	// the blocking go-git call under writeMu. A 30-minute idle timeout
	// tears down workers for quiet projects so long-running servers with
	// ephemeral projects do not accumulate goroutines; the next Enqueue
	// for that project spawns a fresh worker transparently.
	commitQueue := gitops.NewCommitQueue(git, 0, gitops.WithIdleTimeout(30*time.Minute))
	svc.SetCommitQueue(commitQueue)
	slog.Info("commit queue initialized")

	// Create context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// httpCtx is cancelled at the start of shutdown so that long-lived
	// connections (SSE streams) exit immediately instead of holding
	// server.Shutdown hostage until the timeout expires.
	httpCtx, httpCancel := context.WithCancel(ctx)
	defer httpCancel()

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

	// Start Jira integration if configured
	var jiraImporter *jira.Importer
	var jiraWriteBack *jira.WriteBackHandler
	if (cfg.Jira.Token != "" || cfg.Jira.SessionToken != "") && cfg.Jira.BaseURL != "" {
		jiraClient := jira.NewClient(cfg.Jira)
		jiraImporter = jira.NewImporter(jiraClient, svc, store, cfg.Jira)
		jiraWriteBack = jira.NewWriteBackHandler(jiraClient, store, bus)
		jiraWriteBack.Start(ctx)
		slog.Info("jira integration enabled", "base_url", cfg.Jira.BaseURL)
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

	// Create MCP server
	mcpSrv := mcpserver.NewServer(svc, cfg.SkillsDir)

	mcpHandler := mcpserver.NewHandler(mcpSrv, cfg.MCPAPIKey)
	if cfg.MCPAPIKey != "" {
		slog.Info("MCP authentication enabled")
	}

	// Create router with all API routes. MCP is registered on the inner mux
	// so it shares the same middleware chain as every other route — no
	// separate wrapping needed here.
	var apiSyncer api.Syncer
	if syncer != nil {
		apiSyncer = syncer
	}

	mux := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Bus:                bus,
		CORSOrigin:         cfg.CORSOrigin,
		Syncer:             apiSyncer,
		Runner:             runnerClient,
		RunnerCfg:          cfg.Runner,
		JiraImporter:       jiraImporter,
		JiraBaseURL:        cfg.Jira.BaseURL,
		MCPAPIKey:          cfg.MCPAPIKey,
		Port:               cfg.Port,
		GitHubToken:        cfg.GitHub.Token,
		GitHubAPIBaseURL:   cfg.GitHub.ResolvedAPIBaseURL(),
		GitHubAllowedHosts: cfg.GitHub.AllowedHosts(),
		SessionManager:     sessionMgr,
		Theme:              cfg.Theme,
		Version:            buildVersion(),
		MCPHandler:         mcpHandler,
	})

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
		BaseContext:       func(_ net.Listener) context.Context { return httpCtx },
	}

	errCh := make(chan error, 2)

	go func() {
		slog.Info("starting server", "port", cfg.Port)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)

			errCh <- err
		}
	}()

	var adminServer *http.Server

	if cfg.AdminPort > 0 {
		adminBind := cfg.AdminBindAddr
		if adminBind == "" {
			adminBind = "127.0.0.1"
		}

		if adminBind != "127.0.0.1" && adminBind != "localhost" && adminBind != "::1" {
			slog.Warn("admin server bound to non-loopback address — pprof/metrics exposed; restrict via firewall",
				"addr", adminBind, "port", cfg.AdminPort)
		}

		adminServer = &http.Server{
			Addr:              net.JoinHostPort(adminBind, strconv.Itoa(cfg.AdminPort)),
			Handler:           newAdminMux(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		go func() {
			slog.Info("starting admin server", "addr", adminServer.Addr)

			if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("admin server error", "error", err)

				errCh <- err
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
	case err := <-errCh:
		slog.Error("server failed, initiating shutdown", "error", err)
	}

	slog.Info("shutdown: initiated")

	shutdownStart := time.Now()

	// 5s is enough for in-flight REST requests to complete. Long-lived SSE
	// connections are already terminated by httpCancel() before Shutdown is
	// called.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Phase 1: stop accepting new HTTP connections and drain in-flight
	// requests. Cancel httpCtx first so SSE handlers see r.Context().Done()
	// and exit immediately instead of blocking until the shutdown timeout.
	slog.Info("shutdown: phase=http_drain")
	httpCancel()

	var (
		wg              sync.WaitGroup
		mainShutdownErr error
	)

	if adminServer != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := adminServer.Shutdown(shutdownCtx); err != nil {
				slog.Error("admin server shutdown error", "error", err)
			}
		}()
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		mainShutdownErr = server.Shutdown(shutdownCtx)
	}()

	wg.Wait()

	// Phase 2: drain active runner SSE sessions. HTTP is no longer accepting
	// new connections, so closing these pumps is safe — every subscriber
	// receives a terminal SSE event instead of a mid-stream EOF.
	slog.Info("shutdown: phase=sessionlog_close")

	if err := sessionMgr.Close(shutdownCtx); err != nil {
		slog.Error("session manager shutdown error", "error", err)
	}

	// Phase 3: signal the rest of the app (timeout checker, syncers'
	// periodic loops, runner subscribers) to wind down.
	slog.Info("shutdown: phase=ctx_cancel")
	cancel()

	// Phase 4: drain the commit queue so any writes that landed on the
	// worker channel — but whose go-git commit had not yet started when
	// ctx was cancelled — still make it to disk before we exit. Running
	// this before the syncers' Wait ensures the on-disk commits exist to
	// be pushed by a final push iteration.
	slog.Info("shutdown: phase=commit_queue_close")

	if err := commitQueue.Close(shutdownCtx); err != nil {
		slog.Error("commit queue shutdown error", "error", err)
	}

	// Phase 5: let the git syncers finish any late commit/push triggered by
	// requests that were in flight when HTTP drain began. Running this after
	// HTTP drain (not before) ensures those late mutations still get pushed
	// to the remote before we exit.
	//
	// Each syncer.Wait() is bounded by a per-phase deadline so a wedged
	// subprocess (e.g. a git push that ignores the cancelled ctx) cannot hang
	// shutdown past systemd's TimeoutStopSec. The root ctx.cancel() above is
	// still the primary signal; this wait-timeout is the safety net.
	slog.Info("shutdown: phase=syncers_drain")

	const phase5Timeout = 10 * time.Second

	phase5Ctx, phase5Cancel := context.WithTimeout(context.Background(), phase5Timeout)
	defer phase5Cancel()

	if syncer != nil {
		if err := waitSyncer(phase5Ctx, syncer.Wait); err != nil {
			slog.Error("shutdown: gitsync syncer drain exceeded budget",
				"phase", "syncers_drain",
				"timeout", phase5Timeout,
				"error", err,
			)
		}
	}

	if ghSyncer != nil {
		if err := waitSyncer(phase5Ctx, ghSyncer.Wait); err != nil {
			slog.Error("shutdown: github syncer drain exceeded budget",
				"phase", "syncers_drain",
				"timeout", phase5Timeout,
				"error", err,
			)
		}
	}

	// gitops.Manager has no Close method today; if it grows one, call it
	// here after the syncers have finished pushing.

	duration := time.Since(shutdownStart)
	slog.Info("shutdown: complete", "duration", duration)

	if mainShutdownErr != nil {
		slog.Error("server shutdown error", "error", mainShutdownErr)
		os.Exit(1)
	}
}

// waitSyncer wraps a blocking Wait() call with a context deadline. It runs
// Wait() in a goroutine and returns nil as soon as it returns, or ctx.Err()
// if the deadline fires first. The goroutine is leaked in the timeout case —
// acceptable at shutdown because the process exits shortly after.
func waitSyncer(ctx context.Context, wait func()) error {
	done := make(chan struct{})

	go func() {
		defer close(done)

		wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newAdminMux returns a mux serving /debug/pprof/* and /metrics. The mux is
// intentionally scoped: it never mounts http.DefaultServeMux so nothing else
// that imports net/http/pprof (or blindly calls http.Handle) can leak through
// the admin listener.
func newAdminMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	mux.Handle("GET /metrics", promhttp.Handler())

	return mux
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

		// Admin endpoints live on a separate listener; serving the SPA shell
		// for them on the main port would be confusing. 404 explicitly so
		// operators scraping the wrong port get a clear signal.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
			http.NotFound(w, r)

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
